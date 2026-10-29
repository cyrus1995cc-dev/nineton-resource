package app

import (
	"context"
	"database/sql"
	"net/http"
	"sort"
)

type employeeBinding struct {
	code                string
	employeeID, version int64
}

func bindMappedEmployee(ctx context.Context, tx *sql.Tx, binding employeeBinding, reason string, u Row) bool {
	asset := one(ctx, tx, "SELECT * FROM assets WHERE asset_code=? FOR UPDATE", binding.code)
	employee := one(ctx, tx, "SELECT * FROM employees WHERE id=? AND active=1 AND missing_since IS NULL FOR SHARE", binding.employeeID)
	if asset == nil || employee == nil {
		fail(409, binding.code+"：资产或有效员工已变化，请刷新后重新核对")
	}
	if asset["assignee"] != employee["name"] || str(asset["assignee"]) == "" {
		fail(409, binding.code+"：旧台账姓名与员工姓名不一致，请通过交接审批变更")
	}
	if num(asset["assignee_employee_id"]) == binding.employeeID {
		return false
	}
	if num(asset["version"]) != binding.version {
		fail(409, binding.code+"：台账已变化，请刷新后重新核对")
	}
	if asset["assignee_employee_id"] != nil {
		fail(409, binding.code+"：已关联其他员工，更换使用人请通过交接审批")
	}
	if exists(ctx, tx, "SELECT i.id FROM asset_flow_items i JOIN asset_flows f ON f.id=i.flow_id WHERE i.asset_id=? AND f.status='pending'", asset["id"]) {
		fail(409, binding.code+"：有待审批单据，请先处理后关联")
	}
	exec(ctx, tx, "UPDATE assets SET assignee_employee_id=?,version=version+1,updated_at_system=CURRENT_TIMESTAMP(3) WHERE id=?", binding.employeeID, asset["id"])
	writeEvent(ctx, tx, asset, "人员关联", str(u["display_name"]), reason, nil, nil, []Row{{"field": "assignee_employee_id", "before": "", "after": binding.employeeID}})
	audit(ctx, tx, u, "bind_employee", binding.code, Row{"employeeId": binding.employeeID, "reason": reason})
	return true
}

func (a *App) bindEmployeesBatch(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	b := body(r)
	reason := text(b["reason"], 500, true)
	items, ok := b["items"].([]any)
	if !ok || len(items) == 0 || len(items) > 2000 {
		fail(400, "每次请选择 1 至 2000 件资产")
	}
	bindings := make([]employeeBinding, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		v, ok := item.(map[string]any)
		if !ok {
			fail(400, "关联数据格式无效")
		}
		code := text(v["assetCode"], 100, true)
		if seen[code] {
			fail(400, "资产编码不能重复")
		}
		seen[code] = true
		bindings = append(bindings, employeeBinding{code: code, employeeID: integer(v["employeeId"], 0, 9007199254740991), version: integer(v["expectedVersion"], 0, 9007199254740991)})
	}
	// Match the lock order used by asset flows. Any conflict rolls back the batch.
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].code < bindings[j].code })
	result := transaction(r.Context(), a.DB, func(tx *sql.Tx) Row {
		bound := 0
		for _, binding := range bindings {
			if bindMappedEmployee(r.Context(), tx, binding, reason, actor(r)) {
				bound++
			}
		}
		return Row{"bound": bound, "skipped": len(bindings) - bound}
	})
	send(w, 200, result)
}
