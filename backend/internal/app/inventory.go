package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
)

func inventoryDetail(ctx context.Context, q DBTX, id int64) Row {
	task := one(ctx, q, "SELECT * FROM inventory_tasks WHERE id=?", id)
	if task == nil {
		fail(404, "盘点任务不存在")
	}
	return Row{"task": task, "records": rows(ctx, q, "SELECT * FROM inventory_records WHERE task_id=? ORDER BY id", id)}
}
func (a *App) inventoryTasks(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager", "viewer")
	send(w, 200, Row{"tasks": rows(r.Context(), a.DB, "SELECT * FROM inventory_tasks ORDER BY id DESC")})
}
func (a *App) getInventory(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager", "viewer")
	send(w, 200, inventoryDetail(r.Context(), a.DB, routeID(r, "id")))
}
func (a *App) createInventory(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	b := body(r)
	name := text(b["name"], 200, true)
	location := text(b["location"], 200, false)
	department := text(b["department"], 200, false)
	ctx := r.Context()
	v := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		assets := rows(ctx, tx, "SELECT * FROM assets WHERE (?='' OR location=?) AND (?='' OR department=?) ORDER BY id FOR SHARE", location, location, department, department)
		if len(assets) == 0 {
			fail(400, "没有符合条件的资产")
		}
		id := insert(ctx, tx, "INSERT INTO inventory_tasks (name,location_filter,department_filter,operator_name,total_count) VALUES (?,?,?,?,?)", name, location, department, actor(r)["display_name"], len(assets))
		for _, v := range assets {
			exec(ctx, tx, "INSERT INTO inventory_records (task_id,asset_id,asset_code,asset_name,expected_status,expected_location,expected_assignee,expected_department) VALUES (?,?,?,?,?,?,?,?)", id, v["id"], v["asset_code"], str(v["name"]), v["status"], str(v["location"]), str(v["assignee"]), str(v["department"]))
		}
		out := inventoryDetail(ctx, tx, id)
		out["status"] = "created"
		return out
	})
	send(w, 201, v)
}
func inventoryAudit(ctx context.Context, q DBTX, task, record Row, result, location, note, operator string) {
	label := map[string]string{"missing": "未盘到", "different": "位置差异", "found": "正常"}[result]
	description := fmt.Sprintf("盘点任务 #%v「%v」：%s；应在 %v", task["id"], task["name"], label, record["expected_location"])
	if result != "missing" {
		description += "；实盘位置 " + location
	}
	if note != "" {
		description += "；备注：" + note
	}
	old := one(ctx, q, "SELECT * FROM assets WHERE id=?", record["asset_id"])
	writeEvent(ctx, q, old, "盘点", operator, description, nil, nil, nil)
}
func refreshInventory(ctx context.Context, q DBTX, id int64) {
	exec(ctx, q, "UPDATE inventory_tasks SET checked_count=(SELECT COUNT(*) FROM inventory_records WHERE task_id=? AND result IN ('found','different')),difference_count=(SELECT COUNT(*) FROM inventory_records WHERE task_id=? AND result IN ('different','missing')) WHERE id=?", id, id, id)
}
func (a *App) scanInventory(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	id := routeID(r, "id")
	b := body(r)
	code := text(b["assetCode"], 100, true)
	note := text(b["note"], 2000, false)
	ctx := r.Context()
	v := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		task := one(ctx, tx, "SELECT * FROM inventory_tasks WHERE id=? FOR UPDATE", id)
		if task == nil {
			fail(404, "盘点任务不存在")
		}
		if task["status"] == "completed" {
			fail(409, "盘点任务已结束")
		}
		record := one(ctx, tx, "SELECT * FROM inventory_records WHERE task_id=? AND asset_code=? FOR UPDATE", id, code)
		if record == nil {
			fail(400, "资产不在本次盘点范围内")
		}
		location := str(record["expected_location"])
		if raw, ok := b["actualLocation"]; ok {
			location = text(raw, 200, false)
		}
		state := "found"
		if location != str(record["expected_location"]) {
			state = "different"
		}
		replay := record["result"] == state && str(record["actual_location"]) == location && str(record["note"]) == note
		if !replay {
			exec(ctx, tx, "UPDATE inventory_records SET result=?,actual_location=?,note=?,operator_name=?,checked_at=CURRENT_TIMESTAMP(3) WHERE id=?", state, location, note, actor(r)["display_name"], record["id"])
			inventoryAudit(ctx, tx, task, record, state, location, note, str(actor(r)["display_name"]))
			refreshInventory(ctx, tx, id)
		}
		out := inventoryDetail(ctx, tx, id)
		out["status"] = "recorded"
		out["replayed"] = replay
		out["record"] = one(ctx, tx, "SELECT * FROM inventory_records WHERE id=?", record["id"])
		return out
	})
	send(w, 200, v)
}
func (a *App) completeInventory(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	id := routeID(r, "id")
	ctx := r.Context()
	v := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		task := one(ctx, tx, "SELECT * FROM inventory_tasks WHERE id=? FOR UPDATE", id)
		if task == nil {
			fail(404, "盘点任务不存在")
		}
		if task["status"] != "completed" {
			for _, record := range rows(ctx, tx, "SELECT * FROM inventory_records WHERE task_id=? AND result='pending' ORDER BY id", id) {
				inventoryAudit(ctx, tx, task, record, "missing", "", "", str(actor(r)["display_name"]))
			}
			exec(ctx, tx, "UPDATE inventory_records SET result='missing',operator_name=?,checked_at=NULL WHERE task_id=? AND result='pending'", actor(r)["display_name"], id)
			exec(ctx, tx, "UPDATE inventory_tasks SET status='completed',completed_at=CURRENT_TIMESTAMP(3) WHERE id=?", id)
			refreshInventory(ctx, tx, id)
		}
		out := inventoryDetail(ctx, tx, id)
		out["status"] = "completed"
		return out
	})
	send(w, 200, v)
}
