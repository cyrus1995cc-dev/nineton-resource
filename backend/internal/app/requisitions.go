package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"time"
)

func (a *App) fixedApprover(ctx context.Context, q DBTX) Row {
	if a.Config.FixedApproverID < 1 {
		fail(503, "尚未设置固定资产管理员")
	}
	u := one(ctx, q, userJoin+"WHERE u.id=? AND u.role='admin' AND "+validUser, a.Config.FixedApproverID)
	if u == nil {
		fail(409, "固定资产管理员不可用，请联系管理员")
	}
	return publicUser(u)
}

func (a *App) requisitionOptions(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager", "employee", "approver")
	admin := a.fixedApprover(r.Context(), a.DB)
	// Categories only: employees must not receive inventory counts or stock details.
	categories := rows(r.Context(), a.DB, "SELECT c.id,c.name,c.path FROM asset_categories c JOIN asset_card_types t ON t.id=c.card_type_id WHERE c.active=1 AND t.active=1 AND NOT EXISTS(SELECT 1 FROM asset_categories sub WHERE sub.parent_id=c.id AND sub.active=1) AND (c.parent_id IS NULL OR EXISTS(SELECT 1 FROM asset_categories p WHERE p.id=c.parent_id AND p.active=1)) ORDER BY c.path")
	send(w, 200, Row{"categories": categories, "approver": admin, "dingtalk": a.oaEnabled()})
}

func (a *App) createRequisition(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager", "employee", "approver")
	b, ctx, u := body(r), r.Context(), actor(r)
	categoryID, quantity := integer(b["categoryId"], 0, 9007199254740991), integer(b["quantity"], 0, 20)
	purpose, needed, requestID := text(b["purpose"], 2000, true), text(b["neededDate"], 10, true), text(b["requestId"], 100, true)
	day, err := time.Parse("2006-01-02", needed)
	if err != nil || day.Format("2006-01-02") != needed {
		fail(400, "请选择有效的需求日期")
	}
	input := Row{"categoryId": categoryID, "quantity": quantity, "purpose": purpose, "neededDate": needed, "applicantId": u["id"]}
	digest, replayed := hash(js(input)), false
	v := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		one(ctx, tx, "SELECT id FROM users WHERE id=? FOR UPDATE", u["id"])
		if old := one(ctx, tx, "SELECT id,request_hash FROM asset_flows WHERE request_id=?", requestID); old != nil {
			if old["request_hash"] != digest {
				fail(409, "请求标识已用于另一份申请")
			}
			replayed = true
			return flowDetail(ctx, tx, num(old["id"]))
		}
		e := one(ctx, tx, "SELECT * FROM employees WHERE id=? AND active=1 AND missing_since IS NULL FOR SHARE", u["employee_id"])
		if e == nil {
			fail(403, "请先关联有效员工身份")
		}
		category := assetCategory(ctx, tx, categoryID)
		if exists(ctx, tx, "SELECT id FROM asset_categories WHERE parent_id=? AND active=1", categoryID) {
			fail(400, "请选择具体的设备分类")
		}
		admin := a.fixedApprover(ctx, tx)
		id := insert(ctx, tx, "INSERT INTO asset_flows(document_no,type,applicant_id,applicant_name,applicant_department,to_assignee,to_department,to_employee_id,note,request_id,request_hash) VALUES (?,'领用',?,?,?,?,?,?,?,?,?)", "pending:"+requestID, u["id"], u["display_name"], str(e["department"]), e["name"], str(e["department"]), e["id"], purpose, requestID, digest)
		exec(ctx, tx, "UPDATE asset_flows SET document_no=? WHERE id=?", fmt.Sprintf("ZCXY%s%05d", time.Now().In(time.FixedZone("CST", 28800)).Format("20060102"), id), id)
		exec(ctx, tx, "INSERT INTO asset_requisitions(flow_id,category_id,category_path,quantity,needed_date,purpose) VALUES (?,?,?,?,?,?)", id, categoryID, category["path"], quantity, needed, purpose)
		exec(ctx, tx, "INSERT INTO asset_flow_steps(flow_id,sequence,approver_id,approver_name,status) VALUES (?,1,?,?,'pending')", id, admin["id"], admin["display_name"])
		flowLog(ctx, tx, id, "submitted", u, purpose)
		f := one(ctx, tx, "SELECT * FROM asset_flows WHERE id=?", id)
		notify(ctx, tx, f, "approval_pending", admin["id"], str(admin["display_name"]), 1)
		a.queueOA(ctx, tx, f)
		return flowDetail(ctx, tx, id)
	})
	status := 201
	if replayed {
		status = 200
	}
	send(w, status, v)
}

func (a *App) allocateRequisition(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	ctx, u, id, b := r.Context(), actor(r), routeID(r, "id"), body(r)
	if num(u["id"]) != a.Config.FixedApproverID {
		fail(403, "只有固定资产管理员可分配设备")
	}
	raw, ok := b["assetCodes"].([]any)
	if !ok || len(raw) < 1 || len(raw) > 20 {
		fail(400, "请选择1至20件空闲设备")
	}
	codes, seen := []string{}, map[string]bool{}
	for _, v := range raw {
		code := text(v, 100, true)
		if !codePattern.MatchString(code) || seen[code] {
			fail(400, "资产编码无效或重复")
		}
		seen[code] = true
		codes = append(codes, code)
	}
	sort.Strings(codes)
	v := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		f := one(ctx, tx, "SELECT * FROM asset_flows WHERE id=? FOR UPDATE", id)
		demand := one(ctx, tx, "SELECT * FROM asset_requisitions WHERE flow_id=? FOR UPDATE", id)
		if f == nil || demand == nil {
			fail(404, "领用需求不存在")
		}
		version := integer(b["expectedVersion"], 0, 9007199254740991)
		if num(demand["version"]) != version {
			fail(409, "分配信息已变化，请刷新核对")
		}
		if f["status"] != "pending" && f["status"] != "approved" {
			fail(409, "单据已结束")
		}
		if int64(len(codes)) != num(demand["quantity"]) {
			fail(400, "分配数量必须与申请数量一致")
		}
		if !exists(ctx, tx, "SELECT id FROM employees WHERE id=? AND active=1 AND missing_since IS NULL FOR SHARE", f["to_employee_id"]) {
			fail(409, "领用员工已停用或不在通讯录范围")
		}
		assets := []Row{}
		for _, code := range codes {
			asset := one(ctx, tx, "SELECT * FROM assets WHERE asset_code=? FOR UPDATE", code)
			if asset == nil || asset["status"] != "空闲" || asset["assignee_employee_id"] != nil || str(asset["assignee"]) != "" {
				fail(409, "设备不存在或不是未分配的空闲设备："+code)
			}
			if str(asset["category_path"]) != str(demand["category_path"]) {
				fail(400, "设备分类与申请不符："+code)
			}
			if exists(ctx, tx, "SELECT i.id FROM asset_flow_items i JOIN asset_flows f ON f.id=i.flow_id WHERE i.asset_id=? AND f.status IN ('pending','approved') AND f.id<>?", asset["id"], id) {
				fail(409, "设备已被其他单据占用："+code)
			}
			assets = append(assets, asset)
		}
		exec(ctx, tx, "DELETE FROM asset_flow_items WHERE flow_id=?", id)
		for _, v := range assets {
			exec(ctx, tx, "INSERT INTO asset_flow_items(flow_id,asset_id,asset_code,asset_name,category_path,model,expected_status,expected_location,expected_assignee,expected_department,expected_employee_id) VALUES (?,?,?,?,?,?,?,?,?,?,?)", id, v["id"], v["asset_code"], str(v["name"]), str(v["category_path"]), str(v["model"]), v["status"], str(v["location"]), str(v["assignee"]), str(v["department"]), v["assignee_employee_id"])
		}
		exec(ctx, tx, "UPDATE asset_requisitions SET version=version+1 WHERE flow_id=?", id)
		flowLog(ctx, tx, id, "allocated", u, fmt.Sprintf("已分配%d件设备", len(codes)))
		if f["status"] == "approved" {
			a.finishFlow(ctx, tx, f, u)
		}
		return flowDetail(ctx, tx, id)
	})
	send(w, 200, v)
}
