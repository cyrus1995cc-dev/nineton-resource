package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var flowPrefixes = map[string]string{"领用": "ZCLY", "归还": "ZCTH", "借出": "ZCJC", "借出归还": "ZCJH", "调拨": "ZCDB", "交接": "ZCJJ", "维修": "ZCWX", "报废": "ZCBF", "报失": "ZCBS"}

// Preserve the original JSON property order so migrated pending requests remain idempotent.
func requestDigest(input Row) string {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, key := range []string{"type", "assetCodes", "applicantDepartment", "approverIds", "toAssignee", "toDepartment", "toLocation", "note", "requestId", "toEmployeeId", "applicantId"} {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(key))
		b.WriteByte(':')
		var value bytes.Buffer
		encoder := json.NewEncoder(&value)
		encoder.SetEscapeHTML(false)
		must(encoder.Encode(input[key]))
		b.Write(bytes.TrimSuffix(value.Bytes(), []byte{'\n'}))
	}
	b.WriteByte('}')
	return hash(b.String())
}

func available(status string) []string {
	switch status {
	case "空闲":
		return []string{"领用", "借出", "调拨", "维修", "报废", "报失"}
	case "在用":
		return []string{"归还", "交接", "调拨", "维修", "报废", "报失"}
	case "借出":
		return []string{"借出归还", "交接", "调拨", "维修", "报废", "报失"}
	case "维修":
		return []string{"调拨", "报废", "报失"}
	}
	return nil
}
func approvers(ctx context.Context, q DBTX) []Row {
	return rows(ctx, q, "SELECT u.id,u.username,u.display_name,u.role FROM users u LEFT JOIN employees e ON e.id=u.employee_id WHERE "+validUser+" AND u.role IN ('admin','asset_manager','approver') ORDER BY u.id")
}
func (a *App) flowOptions(w http.ResponseWriter, r *http.Request) {
	send(w, 200, Row{"approvers": approvers(r.Context(), a.DB), "employees": employeeList(r.Context(), a.DB, "", false), "notification": Row{"mode": "queued", "message": "通知已接入队列，钉钉发送待配置联调"}})
}
func flowWhere(u Row, f url.Values) (string, []any) {
	clauses := []string{"1=1"}
	args := []any{}
	if restricted(u) {
		clauses = append(clauses, "(f.applicant_id=? OR EXISTS (SELECT 1 FROM asset_flow_steps s WHERE s.flow_id=f.id AND s.approver_id=?) OR f.to_employee_id=?)")
		emp := u["employee_id"]
		if emp == nil {
			emp = -1
		}
		args = append(args, u["id"], u["id"], emp)
	}
	switch f.Get("scope") {
	case "mine":
		clauses = append(clauses, "f.applicant_id=?")
		args = append(args, u["id"])
	case "todo":
		clauses = append(clauses, "f.status='pending' AND EXISTS (SELECT 1 FROM asset_flow_steps s WHERE s.flow_id=f.id AND s.approver_id=? AND s.status='pending')")
		args = append(args, u["id"])
	}
	if s := f.Get("status"); s != "" {
		clauses = append(clauses, "f.status=?")
		args = append(args, s)
	}
	if s := f.Get("search"); s != "" {
		clauses = append(clauses, "(f.document_no COLLATE utf8mb4_0900_ai_ci LIKE ? OR f.applicant_name COLLATE utf8mb4_0900_ai_ci LIKE ? OR EXISTS (SELECT 1 FROM asset_flow_items i WHERE i.flow_id=f.id AND (i.asset_code COLLATE utf8mb4_0900_ai_ci LIKE ? OR i.asset_name COLLATE utf8mb4_0900_ai_ci LIKE ?)))")
		for i := 0; i < 4; i++ {
			args = append(args, "%"+s+"%")
		}
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}
func canViewFlow(ctx context.Context, q DBTX, id int64, u Row) bool {
	if !restricted(u) {
		return true
	}
	return exists(ctx, q, "SELECT f.id FROM asset_flows f WHERE id=? AND (f.applicant_id=? OR EXISTS (SELECT 1 FROM asset_flow_steps s WHERE s.flow_id=f.id AND s.approver_id=?) OR f.to_employee_id=?)", id, u["id"], u["id"], u["employee_id"])
}
func flowDetail(ctx context.Context, q DBTX, id int64) Row {
	f := one(ctx, q, "SELECT f.*,(SELECT COUNT(*) FROM asset_flow_items WHERE flow_id=f.id) AS asset_count FROM asset_flows f WHERE f.id=?", id)
	if f == nil {
		fail(404, "单据不存在")
	}
	out := Row{"flow": f}
	for key, table := range map[string]string{"items": "asset_flow_items", "steps": "asset_flow_steps", "logs": "asset_flow_logs", "comments": "asset_flow_comments", "notifications": "notification_outbox"} {
		order := "id"
		if key == "steps" {
			order = "sequence"
		}
		fields := "*"
		if key == "notifications" {
			fields = "id,kind,recipient_name,status,created_at"
		}
		out[key] = rows(ctx, q, "SELECT "+fields+" FROM "+table+" WHERE flow_id=? ORDER BY "+order, id)
	}
	return out
}
func (a *App) listFlows(w http.ResponseWriter, r *http.Request) {
	where, args := flowWhere(actor(r), r.URL.Query())
	p := page(r, "page", 1, 1000000)
	total := one(r.Context(), a.DB, "SELECT COUNT(*) AS n FROM asset_flows f"+where, args...)["n"]
	qargs := append(append([]any{}, args...), 20, (p-1)*20)
	send(w, 200, Row{"items": rows(r.Context(), a.DB, "SELECT f.*,(SELECT COUNT(*) FROM asset_flow_items WHERE flow_id=f.id) AS asset_count FROM asset_flows f"+where+" ORDER BY f.id DESC LIMIT ? OFFSET ?", qargs...), "total": total, "page": p, "pageSize": 20})
}
func (a *App) getFlow(w http.ResponseWriter, r *http.Request) {
	id := routeID(r, "id")
	if !canViewFlow(r.Context(), a.DB, id, actor(r)) {
		fail(404, "单据不存在或无权查看")
	}
	send(w, 200, flowDetail(r.Context(), a.DB, id))
}
func flowLog(ctx context.Context, q DBTX, id int64, action string, u Row, note string) {
	exec(ctx, q, "INSERT INTO asset_flow_logs (flow_id,action,actor_id,actor_name,note) VALUES (?,?,?,?,?)", id, action, u["id"], u["display_name"], note)
}
func notify(ctx context.Context, q DBTX, f Row, kind string, recipient any, name string, step int64) {
	identity := one(ctx, q, "SELECT e.id AS employeeId,e.corp_id AS corpId,e.dingtalk_user_id AS userId FROM users u LEFT JOIN employees e ON e.id=u.employee_id WHERE u.id=?", recipient)
	payload := Row{"flowId": f["id"], "documentNo": f["document_no"], "type": f["type"], "applicant": f["applicant_name"], "path": fmt.Sprintf("/?flow=%v", f["id"]), "recipient": Row{"localUserId": recipient, "employeeId": identity["employeeId"], "corpId": identity["corpId"], "userId": identity["userId"]}}
	exec(ctx, q, "INSERT INTO notification_outbox (flow_id,kind,recipient_id,recipient_name,dedupe_key,payload_json) VALUES (?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=id", f["id"], kind, recipient, name, fmt.Sprintf("%v:%s:%d:%v", f["id"], kind, step, recipient), js(payload))
}
func (a *App) createFlow(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager", "employee", "approver")
	b := body(r)
	u := actor(r)
	kind := text(b["type"], 100, true)
	if flowPrefixes[kind] == "" {
		fail(400, "请选择有效流转类型")
	}
	if restricted(u) && !contains([]string{"领用", "归还", "交接", "维修", "报失"}, kind) {
		fail(403, "员工不能发起此类管理操作")
	}
	rawCodes, ok := b["assetCodes"].([]any)
	if !ok || len(rawCodes) < 1 || len(rawCodes) > 100 {
		fail(400, "每张单据需要 1 至 100 件资产")
	}
	codes := []string{}
	seen := map[string]bool{}
	for _, v := range rawCodes {
		s := text(v, 100, true)
		if seen[s] {
			fail(400, "资产编码不能重复")
		}
		seen[s] = true
		codes = append(codes, s)
	}
	rawApprovers, ok := b["approverIds"].([]any)
	if !ok || len(rawApprovers) < 1 || len(rawApprovers) > 10 {
		fail(400, "请指定 1 至 10 位审批人")
	}
	ids := []int64{}
	idSeen := map[int64]bool{}
	for _, v := range rawApprovers {
		id := integer(v, 0, 9007199254740991)
		if id == num(u["id"]) || idSeen[id] {
			fail(400, "申请人不能审批本人申请，审批人不能重复")
		}
		idSeen[id] = true
		ids = append(ids, id)
	}
	input := Row{"type": kind, "assetCodes": codes, "approverIds": ids, "applicantDepartment": text(b["applicantDepartment"], 200, false), "toAssignee": text(b["toAssignee"], 200, false), "toDepartment": text(b["toDepartment"], 200, false), "toLocation": text(b["toLocation"], 200, false), "note": text(b["note"], 2000, false), "requestId": text(b["requestId"], 100, true), "toEmployeeId": nil, "applicantId": u["id"]}
	if !contains([]string{"领用", "借出", "交接"}, kind) {
		input["toAssignee"] = ""
	}
	if !contains([]string{"领用", "借出", "交接", "调拨"}, kind) {
		input["toDepartment"] = ""
	}
	if b["toEmployeeId"] != nil && contains([]string{"领用", "借出", "交接"}, kind) {
		input["toEmployeeId"] = integer(b["toEmployeeId"], 0, 9007199254740991)
	}
	ctx := r.Context()
	replayed := false
	result := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		one(ctx, tx, "SELECT id FROM users WHERE id=? FOR UPDATE", u["id"])
		if restricted(u) && !exists(ctx, tx, "SELECT id FROM employees WHERE id=? AND active=1 AND missing_since IS NULL", u["employee_id"]) {
			fail(403, "请管理员先关联有效员工身份")
		}
		if input["toEmployeeId"] != nil {
			e := one(ctx, tx, "SELECT * FROM employees WHERE id=? AND active=1 AND missing_since IS NULL FOR SHARE", input["toEmployeeId"])
			if e == nil {
				fail(400, "接收员工不存在或已停用")
			}
			input["toAssignee"] = e["name"]
			input["toDepartment"] = str(e["department"])
		}
		if restricted(u) {
			input["applicantDepartment"] = str(u["department"])
			if kind == "领用" && num(input["toEmployeeId"]) != num(u["employee_id"]) {
				fail(403, "员工领用的接收人必须为本人")
			}
		}
		if contains([]string{"领用", "借出", "交接"}, kind) {
			if input["toEmployeeId"] == nil && exists(ctx, tx, "SELECT id FROM employees WHERE active=1 LIMIT 1") {
				fail(400, "请从通讯录选择接收员工")
			}
			if input["toAssignee"] == "" || input["toDepartment"] == "" {
				fail(400, "请填写目标使用人和使用部门")
			}
		}
		if input["applicantDepartment"] == "" {
			fail(400, "请填写申请部门")
		}
		if kind == "调拨" && input["toLocation"] == "" && input["toDepartment"] == "" {
			fail(400, "调拨需要填写目标位置或部门")
		}
		if contains([]string{"维修", "报失", "报废"}, kind) && input["note"] == "" {
			fail(400, "请填写申请原因或故障说明")
		}
		digest := requestDigest(input)
		if old := one(ctx, tx, "SELECT * FROM asset_flows WHERE request_id=?", input["requestId"]); old != nil {
			if old["request_hash"] != digest {
				fail(409, "请求标识已用于另一份申请")
			}
			replayed = true
			v := flowDetail(ctx, tx, num(old["id"]))
			v["replayed"] = true
			return v
		}
		options := approvers(ctx, tx)
		selected := []Row{}
		for _, id := range ids {
			var found Row
			for _, v := range options {
				if num(v["id"]) == id {
					found = v
					break
				}
			}
			if found == nil {
				fail(400, "审批账号不存在、已停用或没有审批权限")
			}
			one(ctx, tx, "SELECT id FROM users WHERE id=? FOR SHARE", id)
			selected = append(selected, found)
		}
		sorted := append([]string{}, codes...)
		sort.Strings(sorted)
		assets := map[string]Row{}
		for _, code := range sorted {
			v := one(ctx, tx, "SELECT * FROM assets WHERE asset_code=? FOR UPDATE", code)
			if v == nil {
				fail(404, "资产不存在："+code)
			}
			if restricted(u) {
				if kind == "领用" {
					if v["status"] != "空闲" || v["assignee_employee_id"] != nil || str(v["assignee"]) != "" {
						fail(403, "无权为该资产发起领用")
					}
				} else if num(v["assignee_employee_id"]) != num(u["employee_id"]) {
					fail(403, "无权为该资产发起申请")
				}
			}
			if !contains(available(str(v["status"])), kind) {
				fail(409, code+" 当前状态不允许"+kind)
			}
			if exists(ctx, tx, "SELECT i.id FROM asset_flow_items i JOIN asset_flows f ON f.id=i.flow_id WHERE i.asset_id=? AND f.status='pending'", v["id"]) {
				fail(409, code+" 已在审批中，请先处理该单据")
			}
			assets[code] = v
		}
		id := insert(ctx, tx, "INSERT INTO asset_flows (document_no,type,applicant_id,applicant_name,applicant_department,to_assignee,to_department,to_location,note,request_id,request_hash,to_employee_id) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)", "pending:"+str(input["requestId"]), kind, u["id"], u["display_name"], input["applicantDepartment"], input["toAssignee"], input["toDepartment"], input["toLocation"], input["note"], input["requestId"], digest, input["toEmployeeId"])
		day := time.Now().In(time.FixedZone("CST", 8*3600)).Format("20060102")
		exec(ctx, tx, "UPDATE asset_flows SET document_no=? WHERE id=?", fmt.Sprintf("%s%s%05d", flowPrefixes[kind], day, id), id)
		for _, code := range codes {
			v := assets[code]
			exec(ctx, tx, "INSERT INTO asset_flow_items (flow_id,asset_id,asset_code,asset_name,category_path,model,expected_status,expected_location,expected_assignee,expected_department,expected_employee_id) VALUES (?,?,?,?,?,?,?,?,?,?,?)", id, v["id"], code, str(v["name"]), str(v["category_path"]), str(v["model"]), v["status"], str(v["location"]), str(v["assignee"]), str(v["department"]), v["assignee_employee_id"])
		}
		for i, v := range selected {
			status := "waiting"
			if i == 0 {
				status = "pending"
			}
			exec(ctx, tx, "INSERT INTO asset_flow_steps (flow_id,sequence,approver_id,approver_name,status) VALUES (?,?,?,?,?)", id, i+1, v["id"], v["display_name"], status)
		}
		flowLog(ctx, tx, id, "submitted", u, str(input["note"]))
		notify(ctx, tx, one(ctx, tx, "SELECT * FROM asset_flows WHERE id=?", id), "approval_pending", selected[0]["id"], str(selected[0]["display_name"]), 1)
		return flowDetail(ctx, tx, id)
	})
	status := 201
	if replayed {
		status = 200
	}
	send(w, status, result)
}
func (a *App) flowAction(w http.ResponseWriter, r *http.Request) {
	id := routeID(r, "id")
	action := r.PathValue("action")
	if !contains([]string{"approve", "reject", "withdraw", "comments"}, action) {
		fail(404, "接口不存在")
	}
	if action == "approve" || action == "reject" {
		roles(r, "admin", "asset_manager", "approver")
	} else {
		roles(r, "admin", "asset_manager", "employee", "approver")
	}
	b := body(r)
	comment := text(b["comment"], 2000, false)
	u := actor(r)
	ctx := r.Context()
	v := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		f := one(ctx, tx, "SELECT * FROM asset_flows WHERE id=? FOR UPDATE", id)
		if f == nil {
			fail(404, "单据不存在")
		}
		if action == "comments" {
			if !canViewFlow(ctx, tx, id, u) {
				fail(404, "单据不存在或无权查看")
			}
			content := text(b["content"], 2000, true)
			exec(ctx, tx, "INSERT INTO asset_flow_comments (flow_id,user_id,user_name,content) VALUES (?,?,?,?)", id, u["id"], u["display_name"], content)
			return flowDetail(ctx, tx, id)
		}
		steps := rows(ctx, tx, "SELECT * FROM asset_flow_steps WHERE flow_id=? ORDER BY sequence", id)
		if action == "withdraw" {
			if num(f["applicant_id"]) != num(u["id"]) {
				fail(403, "只有申请人可以撤回单据")
			}
			if f["status"] == "withdrawn" {
				return flowDetail(ctx, tx, id)
			}
		} else {
			var own Row
			for _, s := range steps {
				if num(s["approver_id"]) == num(u["id"]) {
					own = s
					break
				}
			}
			if own == nil {
				fail(403, "你不是该单据的指定审批人")
			}
			if action == "approve" && own["status"] == "approved" || action == "reject" && own["status"] == "rejected" {
				return flowDetail(ctx, tx, id)
			}
		}
		if f["status"] != "pending" {
			fail(409, "该单据已结束，不能继续处理")
		}
		if action == "withdraw" {
			exec(ctx, tx, "UPDATE asset_flows SET status='withdrawn',completed_at=CURRENT_TIMESTAMP(3) WHERE id=?", id)
			exec(ctx, tx, "UPDATE asset_flow_steps SET status='cancelled' WHERE flow_id=? AND status IN ('pending','waiting')", id)
			flowLog(ctx, tx, id, "withdrawn", u, comment)
			cancelNotifications(ctx, tx, id)
			notify(ctx, tx, f, "withdrawn", f["applicant_id"], str(f["applicant_name"]), 0)
			return flowDetail(ctx, tx, id)
		}
		var step, next Row
		for _, s := range steps {
			if num(s["sequence"]) == num(f["current_step"]) && s["status"] == "pending" {
				step = s
			}
			if num(s["sequence"]) == num(f["current_step"])+1 {
				next = s
			}
		}
		if step == nil || num(step["approver_id"]) != num(u["id"]) {
			fail(403, "请等待前一位审批人处理")
		}
		if action == "reject" && comment == "" {
			fail(400, "请填写驳回原因")
		}
		state := "approved"
		if action == "reject" {
			state = "rejected"
		}
		exec(ctx, tx, "UPDATE asset_flow_steps SET status=?,comment=?,acted_at=CURRENT_TIMESTAMP(3) WHERE id=?", state, comment, step["id"])
		flowLog(ctx, tx, id, state, u, comment)
		cancelNotifications(ctx, tx, id)
		if action == "reject" {
			exec(ctx, tx, "UPDATE asset_flows SET status='rejected',completed_at=CURRENT_TIMESTAMP(3) WHERE id=?", id)
			exec(ctx, tx, "UPDATE asset_flow_steps SET status='cancelled' WHERE flow_id=? AND status='waiting'", id)
			notify(ctx, tx, f, "rejected", f["applicant_id"], str(f["applicant_name"]), 0)
		} else if next != nil {
			exec(ctx, tx, "UPDATE asset_flow_steps SET status='pending' WHERE id=?", next["id"])
			exec(ctx, tx, "UPDATE asset_flows SET current_step=? WHERE id=?", next["sequence"], id)
			notify(ctx, tx, f, "approval_pending", next["approver_id"], str(next["approver_name"]), num(next["sequence"]))
		} else {
			items := rows(ctx, tx, "SELECT * FROM asset_flow_items WHERE flow_id=? ORDER BY asset_id", id)
			for _, item := range items {
				old := one(ctx, tx, "SELECT * FROM assets WHERE id=? FOR UPDATE", item["asset_id"])
				if old == nil {
					fail(409, "资产已不存在")
				}
				for _, field := range []string{"status", "location", "assignee", "department"} {
					if str(old[field]) != str(item["expected_"+field]) {
						fail(409, str(item["asset_code"])+" 台账已变化，请撤回后重新申请")
					}
				}
				if num(old["assignee_employee_id"]) != num(item["expected_employee_id"]) {
					fail(409, "人员关联已变化，请撤回重填")
				}
				if f["to_employee_id"] != nil && !exists(ctx, tx, "SELECT id FROM employees WHERE id=? AND active=1 AND missing_since IS NULL FOR SHARE", f["to_employee_id"]) {
					fail(409, "接收员工已停用或不在授权范围")
				}
				applyFlowEvent(ctx, tx, old, f, u)
			}
			exec(ctx, tx, "UPDATE asset_flows SET status='completed',completed_at=CURRENT_TIMESTAMP(3) WHERE id=?", id)
			flowLog(ctx, tx, id, "completed", u, "全部审批通过，资产台账已更新")
			notify(ctx, tx, f, "completed", f["applicant_id"], str(f["applicant_name"]), 0)
		}
		return flowDetail(ctx, tx, id)
	})
	status := 200
	if action == "comments" {
		status = 201
	}
	send(w, status, v)
}
func cancelNotifications(ctx context.Context, q DBTX, id int64) {
	exec(ctx, q, "UPDATE notification_outbox SET status='cancelled' WHERE flow_id=? AND kind='approval_pending' AND status IN ('pending','failed')", id)
}
func applyFlowEvent(ctx context.Context, q DBTX, old, f, u Row) {
	kind := str(f["type"])
	if !contains(available(str(old["status"])), kind) {
		fail(409, "资产状态不允许该操作")
	}
	next := Row{"status": old["status"], "assignee": str(old["assignee"]), "department": str(old["department"]), "location": str(old["location"]), "assignee_employee_id": old["assignee_employee_id"]}
	switch kind {
	case "领用":
		next["status"] = "在用"
	case "借出":
		next["status"] = "借出"
	case "维修":
		next["status"] = "维修"
	case "报废":
		next["status"] = "报废"
	case "报失":
		next["status"] = "丢失"
	case "归还", "借出归还":
		next["status"] = "空闲"
		next["assignee"] = ""
		next["department"] = ""
		next["assignee_employee_id"] = nil
	}
	if contains([]string{"领用", "借出", "交接"}, kind) {
		next["assignee"] = f["to_assignee"]
		next["assignee_employee_id"] = f["to_employee_id"]
	}
	if str(f["to_department"]) != "" {
		next["department"] = f["to_department"]
	}
	if str(f["to_location"]) != "" {
		next["location"] = f["to_location"]
	}
	changes := []Row{}
	for _, field := range []string{"status", "assignee", "department", "location"} {
		if str(old[field]) != str(next[field]) {
			changes = append(changes, Row{"field": field, "before": str(old[field]), "after": next[field]})
		}
	}
	exec(ctx, q, "UPDATE assets SET status=?,assignee=?,department=?,location=?,assignee_employee_id=?,version=version+1,updated_at_system=CURRENT_TIMESTAMP(3) WHERE id=?", next["status"], next["assignee"], next["department"], next["location"], next["assignee_employee_id"], old["id"])
	writeEvent(ctx, q, old, kind, str(u["display_name"]), str(f["note"]), fmt.Sprintf("flow:%v:%v", f["id"], old["id"]), f["id"], changes)
}
