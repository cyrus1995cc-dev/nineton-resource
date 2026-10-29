package app

import (
	"context"
	"testing"
)

func TestSSOBindingPreservesEstablishedEmployeeAccounts(t *testing.T) {
	a, admin := testApp(t)
	ctx := context.Background()
	login := a.ssoSession(ctx, "https://sso.example.com", "verified-employee", "同名", "")
	source := login["user"].(Row)["id"]
	owner := one(ctx, a.DB, "SELECT id FROM users WHERE username='admin'")["id"]
	employee := insert(ctx, a.DB, "INSERT INTO employees(name) VALUES ('同名')")
	exec(ctx, a.DB, "UPDATE users SET employee_id=? WHERE id=?", employee, source)
	path := "/api/auth/users/" + str(owner) + "/sso-binding"
	admin.call("POST", path, Row{"sourceUserId": source}, 409)
	exec(ctx, a.DB, "UPDATE users SET employee_id=NULL WHERE id=?", source)
	insert(ctx, a.DB, "INSERT INTO asset_flows(document_no,type,applicant_id,request_id,request_hash) VALUES ('HISTORY-TEST','领用',?,'sso-history','test')", source)
	admin.call("POST", path, Row{"sourceUserId": source}, 409)
	if num(one(ctx, a.DB, "SELECT user_id FROM external_identities WHERE subject='verified-employee'")["user_id"]) != num(source) {
		t.Fatal("established identity was moved")
	}
	if a.resolve(ctx, str(login["token"])) == nil {
		t.Fatal("failed binding invalidated unchanged account")
	}
}
