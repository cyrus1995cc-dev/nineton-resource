package app

import (
	"context"
	"fmt"
	"testing"
)

func TestMySQLBatchEmployeeMappings(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	first := insert(ctx, a.DB, "INSERT INTO employees(name,department,dingtalk_user_id,job_number) VALUES ('同名','研发','mapping-first','A001')")
	second := insert(ctx, a.DB, "INSERT INTO employees(name,department,dingtalk_user_id) VALUES ('同名','行政','mapping-second')")
	inactive := insert(ctx, a.DB, "INSERT INTO employees(name,active,dingtalk_user_id) VALUES ('停用',0,'mapping-inactive')")
	missing := insert(ctx, a.DB, "INSERT INTO employees(name,missing_since,dingtalk_user_id) VALUES ('离职',CURRENT_TIMESTAMP(3),'mapping-missing')")
	for i := 1; i <= 7; i++ {
		code := fmt.Sprintf("MAP-%02d", i)
		seed(ctx, a.DB, code, "在用")
		exec(ctx, a.DB, "UPDATE assets SET assignee='同名',department='原部门',company='原公司' WHERE asset_code=?", code)
	}
	exec(ctx, a.DB, "UPDATE assets SET assignee='停用' WHERE asset_code='MAP-05'")
	exec(ctx, a.DB, "UPDATE assets SET assignee='离职' WHERE asset_code='MAP-06'")
	exec(ctx, a.DB, "UPDATE assets SET assignee='从未同步人员' WHERE asset_code='MAP-07'")
	mappings := c.call("GET", "/api/directory/mappings", nil, 200)["assets"].([]any)
	if len(mappings) != 7 {
		t.Fatal(mappings)
	}
	for _, raw := range mappings {
		v := raw.(map[string]any)
		wantRoster := int64(2)
		wantCandidates := 2
		switch v["assignee"] {
		case "停用":
			wantRoster = 1
			wantCandidates = 0
		case "离职", "从未同步人员":
			wantRoster = 0
			wantCandidates = 0
		}
		if num(v["roster_match_count"]) != wantRoster || len(v["candidates"].([]any)) != wantCandidates || v["company"] != "原公司" {
			t.Fatal("classification data", v)
		}
	}
	item := func(code string, employee int64) Row {
		return Row{"assetCode": code, "employeeId": employee, "expectedVersion": 1}
	}
	batch := func(items ...Row) Row { return Row{"items": items, "reason": "核对部门与工号"} }
	path := "/api/directory/mappings/batch"
	payload := batch(item("MAP-01", first), item("MAP-02", second))
	result := c.call("POST", path, payload, 200)
	if num(result["bound"]) != 2 || num(result["skipped"]) != 0 {
		t.Fatal(result)
	}
	result = c.call("POST", path, payload, 200)
	if num(result["bound"]) != 0 || num(result["skipped"]) != 2 {
		t.Fatal("retry", result)
	}
	if num(one(ctx, a.DB, "SELECT COUNT(*) n FROM audit_logs WHERE action='bind_employee'")["n"]) != 2 || num(one(ctx, a.DB, "SELECT COUNT(*) n FROM asset_events WHERE type='人员关联'")["n"]) != 2 {
		t.Fatal("missing or duplicate history")
	}
	for _, code := range []string{"MAP-01", "MAP-02"} {
		v := one(ctx, a.DB, "SELECT * FROM assets WHERE asset_code=?", code)
		if v["assignee"] != "同名" || v["department"] != "原部门" || v["company"] != "原公司" || v["status"] != "在用" || num(v["version"]) != 2 {
			t.Fatal("history changed", v)
		}
	}
	// One stale asset must roll back every earlier update and its audit event.
	exec(ctx, a.DB, "UPDATE assets SET version=version+1 WHERE asset_code='MAP-04'")
	c.call("POST", path, batch(item("MAP-03", first), item("MAP-04", first)), 409)
	if v := one(ctx, a.DB, "SELECT assignee_employee_id,version FROM assets WHERE asset_code='MAP-03'"); v["assignee_employee_id"] != nil || num(v["version"]) != 1 {
		t.Fatal("partial commit", v)
	}
	c.call("POST", path, batch(item("MAP-01", second)), 409)
	c.call("POST", path, batch(item("MAP-03", inactive)), 409)
	c.call("POST", path, batch(item("MAP-05", inactive)), 409)
	c.call("POST", path, batch(item("MAP-06", missing)), 409)
	c.call("POST", path, batch(item("MAP-07", first)), 409)
	c.call("POST", path, batch(item("MAP-03", first), item("MAP-03", first)), 400)
	c.call("POST", path, Row{"items": []Row{}, "reason": "核对"}, 400)
	c.call("POST", path, Row{"items": []Row{item("MAP-03", first)}, "reason": " "}, 400)
	c.call("POST", path, batch(item("MAP-03", 0)), 400)
	// Pending business approvals must be resolved before binding the legacy asset.
	approver := c.call("POST", "/api/auth/users", Row{"username": "mappingapprover", "password": "mapping-approve-pass", "display_name": "审批", "role": "approver"}, 201)["user"].(map[string]any)
	c.call("POST", "/api/flows", Row{"type": "维修", "assetCodes": []string{"MAP-03"}, "applicantDepartment": "行政", "approverIds": []any{approver["id"]}, "note": "检修", "requestId": "mapping-pending"}, 201)
	c.call("POST", path, batch(item("MAP-03", first)), 409)
	for _, role := range []string{"employee", "asset_manager"} {
		user := c.call("POST", "/api/auth/users", Row{"username": "map" + role, "password": "mapping-user-pass", "display_name": "同名", "role": role}, 201)["user"].(map[string]any)
		if role == "employee" {
			exec(ctx, a.DB, "UPDATE users SET employee_id=? WHERE id=?", first, user["id"])
		}
		login := testClient{t: t, handler: a.Handler()}.call("POST", "/api/auth/login", Row{"username": "map" + role, "password": "mapping-user-pass"}, 200)
		client := testClient{t: t, handler: a.Handler(), token: str(login["token"])}
		client.call("POST", path, payload, 403)
		if role == "employee" {
			client.call("GET", "/api/assets/MAP-01", nil, 200)
			client.call("GET", "/api/assets/MAP-02", nil, 404)
		}
	}
}
