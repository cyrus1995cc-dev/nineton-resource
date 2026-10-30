package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func directorySSOEmployee(t *testing.T, a *App, subject, name string) (any, any) {
	t.Helper()
	ctx := context.Background()
	id := insert(ctx, a.DB, "INSERT INTO employees(corp_id,dingtalk_user_id,name) VALUES ('test-corp',?,?)", subject, name)
	ensureEmployeeUser(ctx, a.DB, id)
	return id, one(ctx, a.DB, "SELECT id FROM users WHERE employee_id=?", id)["id"]
}

func expectSSODirectoryProblem(t *testing.T, status int, fn func()) {
	t.Helper()
	defer func() {
		p := recover()
		if issue, ok := p.(problem); !ok || issue.Status != status {
			t.Fatalf("expected status %d, got %v", status, p)
		}
	}()
	fn()
}

func TestSSODirectoryRepairsAllExistingSessions(t *testing.T) {
	a, admin := testApp(t)
	ctx := context.Background()
	a.Config.SSOIssuer = "https://identity.test"
	a.Config.DingCorp = "test-corp"
	type account struct {
		id, employee, placeholder any
		token                     string
		count                     int
	}
	accounts := []account{}
	for i, count := range []int{19, 5, 6} {
		subject := fmt.Sprintf("00%d", i)
		employee, placeholder := directorySSOEmployee(t, a, subject, "同名员工")
		// Reproduce old unlinked SSO accounts before enabling the explicit contract.
		login := a.ssoSession(ctx, a.Config.SSOIssuer, subject, "同名员工", "")
		id := login["user"].(Row)["id"]
		for j := 0; j < count; j++ {
			code := fmt.Sprintf("OWN-%d%d", i, j)
			seed(ctx, a.DB, code, "在用")
			exec(ctx, a.DB, "UPDATE assets SET assignee_employee_id=?,assignee='同名员工' WHERE asset_code=?", employee, code)
		}
		accounts = append(accounts, account{id, employee, placeholder, str(login["token"]), count})
	}
	// Retain canonical user IDs and business history instead of moving identities.
	exec(ctx, a.DB, "INSERT INTO asset_flows(document_no,type,applicant_id,request_id,request_hash) VALUES ('PRESERVE-SSO','领用',?,'preserve-sso','test')", accounts[0].id)
	adminID := one(ctx, a.DB, "SELECT id FROM users WHERE role='admin'")["id"]
	adminEmployee, adminPlaceholder := directorySSOEmployee(t, a, "admin-stable-sub", "管理员")
	exec(ctx, a.DB, "INSERT INTO external_identities(issuer,subject,user_id) VALUES (?, 'admin-stable-sub',?)", a.Config.SSOIssuer, adminID)
	a.Config.SSOSubjectIsDingTalkUserID = true
	a.ReconcileSSODirectory(ctx)
	a.ReconcileSSODirectory(ctx)
	for _, account := range accounts {
		u := a.resolve(ctx, account.token)
		if num(u["id"]) != num(account.id) || num(u["employee_id"]) != num(account.employee) || u["role"] != "employee" {
			t.Fatal("original session not repaired", u)
		}
		client := testClient{t: t, handler: a.Handler(), token: account.token}
		for _, path := range []string{"/api/assets", "/api/assets?scope=mine", "/api/stats", "/api/assets/options?scope=mine"} {
			if got := client.call("GET", path, nil, 200); num(got["total"]) != int64(account.count) {
				t.Fatal("incorrect personal count", path, got)
			}
		}
		client.call("GET", "/api/auth/users", nil, 403)
		other := "OWN-00"
		if account.count == 19 {
			other = "OWN-10"
		}
		client.call("GET", "/api/assets/"+other, nil, 404)
		if p := one(ctx, a.DB, "SELECT active,employee_id FROM users WHERE id=?", account.placeholder); num(p["active"]) != 0 || p["employee_id"] != nil {
			t.Fatal("unused placeholder still owns employee", p)
		}
	}
	if u := a.resolve(ctx, admin.token); u["role"] != "admin" || num(u["employee_id"]) != num(adminEmployee) || num(u["id"]) != num(adminID) {
		t.Fatal("administrator changed", u)
	}
	if num(one(ctx, a.DB, "SELECT active FROM users WHERE id=?", adminPlaceholder)["active"]) != 0 || num(one(ctx, a.DB, "SELECT COUNT(*) n FROM users WHERE active=1 AND role='admin'")["n"]) != 1 {
		t.Fatal("administrator safety")
	}
	if num(one(ctx, a.DB, "SELECT applicant_id FROM asset_flows WHERE request_id='preserve-sso'")["applicant_id"]) != num(accounts[0].id) || num(one(ctx, a.DB, "SELECT COUNT(*) n FROM audit_logs WHERE action='sso_directory_link'")["n"]) != 4 {
		t.Fatal("history or idempotency")
	}
}

func TestSSODirectoryFirstSignedLogin(t *testing.T) {
	a, _ := testApp(t)
	f := mockOIDC(t, a)
	f.subject = "011648522338737463"
	a.Config.DingCorp = "test-corp"
	a.Config.SSOSubjectIsDingTalkUserID = true
	employee, user := directorySSOEmployee(t, a, f.subject, "员工")
	ctx := context.Background()
	state, cookie := oidcStart(t, a, f, "/auth/login")
	if got := oidcCallback(a, state, "forged", cookie); got.Code != 303 || !strings.Contains(got.Header().Get("Location"), "sso_error=invalid") {
		t.Fatal("forged identity accepted", got.Header())
	}
	if exists(ctx, a.DB, "SELECT user_id FROM external_identities") {
		t.Fatal("forged identity linked")
	}
	state, cookie = oidcStart(t, a, f, "/auth/login")
	if got := oidcCallback(a, state, "good", cookie); got.Code != 303 {
		t.Fatal(got.Code, got.Body.String())
	}
	identity := one(ctx, a.DB, "SELECT user_id FROM external_identities WHERE issuer=? AND subject=?", a.Config.SSOIssuer, f.subject)
	if num(identity["user_id"]) != num(user) {
		t.Fatal("created duplicate account", identity)
	}
	u := one(ctx, a.DB, "SELECT * FROM users WHERE id=?", user)
	if num(u["employee_id"]) != num(employee) || u["role"] != "employee" {
		t.Fatal(u)
	}
	login := a.ssoSession(ctx, a.Config.SSOIssuer, f.subject, "管理员", "")
	if login["user"].(Row)["role"] != "employee" {
		t.Fatal("claim elevated user")
	}
}

func TestSSODirectoryDoesNotGuessOrTakeOverAccounts(t *testing.T) {
	for _, scenario := range []string{"wrong-issuer", "wrong-corp", "different-subject", "inactive", "missing", "privileged", "used-placeholder", "conflicting-employee"} {
		t.Run(scenario, func(t *testing.T) {
			a, _ := testApp(t)
			ctx := context.Background()
			a.Config.SSOIssuer = "https://identity.test"
			a.Config.DingCorp = "test-corp"
			employee, user := directorySSOEmployee(t, a, "00123", "同名")
			issuer, subject := a.Config.SSOIssuer, "00123"
			var current any
			if scenario == "used-placeholder" || scenario == "conflicting-employee" {
				login := a.ssoSession(ctx, issuer, subject, "同名", "")
				current = login["user"].(Row)["id"]
			}
			a.Config.SSOSubjectIsDingTalkUserID = true
			switch scenario {
			case "wrong-issuer":
				issuer = "https://other.test"
			case "wrong-corp":
				a.Config.DingCorp = "other-corp"
			case "different-subject":
				subject = "123" // Leading zeroes are identity, never convert to a number.
			case "inactive":
				exec(ctx, a.DB, "UPDATE employees SET active=0 WHERE id=?", employee)
			case "missing":
				exec(ctx, a.DB, "UPDATE employees SET missing_since='2026-10-30' WHERE id=?", employee)
			case "privileged":
				exec(ctx, a.DB, "UPDATE users SET role='admin' WHERE id=?", user)
			case "used-placeholder":
				exec(ctx, a.DB, "INSERT INTO asset_flows(document_no,type,applicant_id,request_id,request_hash) VALUES ('USED-DIRECTORY','领用',?,'used-directory','test')", user)
			case "conflicting-employee":
				other := insert(ctx, a.DB, "INSERT INTO employees(dingtalk_user_id,name) VALUES ('different','同名')")
				exec(ctx, a.DB, "UPDATE users SET employee_id=? WHERE id=?", other, current)
			}
			if scenario == "wrong-issuer" || scenario == "wrong-corp" || scenario == "different-subject" {
				login := a.ssoSession(ctx, issuer, subject, "同名", "")
				if login["user"].(Row)["employee_id"] != nil {
					t.Fatal("guessed ownership", login)
				}
				return
			}
			status := 409
			if scenario == "inactive" || scenario == "missing" {
				status = 403
			}
			expectSSODirectoryProblem(t, status, func() { a.ssoSession(ctx, issuer, subject, "同名", "") })
			if scenario == "used-placeholder" {
				a.ReconcileSSODirectory(ctx)
				if u := one(ctx, a.DB, "SELECT * FROM users WHERE id=?", user); num(u["active"]) != 1 || num(u["employee_id"]) != num(employee) {
					t.Fatal("conflict mutated account", u)
				}
			}
		})
	}
}
