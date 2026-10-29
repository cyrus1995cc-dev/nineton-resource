package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMySQLApprovalRollbackAndConcurrency(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	seed(ctx, a.DB, "ZC-101", "空闲")
	seed(ctx, a.DB, "ZC-102", "空闲")
	user := c.call("POST", "/api/auth/users", Row{"username": "reviewer", "password": "password1", "display_name": "审批人", "role": "approver"}, 201)["user"].(map[string]any)
	anon := testClient{t: t, handler: a.Handler()}
	login := anon.call("POST", "/api/auth/login", Row{"username": "reviewer", "password": "password1"}, 200)
	reviewer := testClient{t: t, handler: a.Handler(), token: str(login["token"])}
	flow := c.call("POST", "/api/flows", Row{"type": "领用", "assetCodes": []string{"ZC-101", "ZC-102"}, "applicantDepartment": "行政", "toAssignee": "人员", "toDepartment": "行政", "approverIds": []any{user["id"]}, "requestId": "rollback-case"}, 201)["flow"].(map[string]any)
	id := num(flow["id"])
	exec(ctx, a.DB, "UPDATE assets SET location='changed',version=version+1 WHERE asset_code='ZC-102'")
	path := fmt.Sprintf("/api/flows/%d/approve", id)
	reviewer.call("POST", path, Row{}, 409)
	first := one(ctx, a.DB, "SELECT * FROM assets WHERE asset_code='ZC-101'")
	if first["status"] != "空闲" {
		t.Fatal("partial asset update survived rollback")
	}
	if one(ctx, a.DB, "SELECT status FROM asset_flow_steps WHERE flow_id=?", id)["status"] != "pending" {
		t.Fatal("approval step survived rollback")
	}
	if exists(ctx, a.DB, "SELECT id FROM asset_events WHERE flow_id=?", id) {
		t.Fatal("event survived rollback")
	}
	exec(ctx, a.DB, "UPDATE assets SET location='办公室',version=version+1 WHERE asset_code='ZC-102'")
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", path, strings.NewReader("{}"))
			r.Header.Set("Authorization", "Bearer "+reviewer.token)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Errorf("approve %d %s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	if n := num(one(ctx, a.DB, "SELECT COUNT(*) AS n FROM asset_events WHERE flow_id=?", id)["n"]); n != 2 {
		t.Fatal("duplicate final approvals", n)
	}
	r := httptest.NewRequest("GET", "/api/exports/flows", nil)
	r.Header.Set("Authorization", "Bearer "+c.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestMySQLDirectoryAndEmployeeScope(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	failUpstream := false
	omitEmployee := false
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failUpstream {
			send(w, 500, Row{"message": "unavailable"})
			return
		}
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			send(w, 200, Row{"accessToken": "app-token", "expireIn": 7200})
		case "/topapi/v2/department/get":
			send(w, 200, Row{"errcode": 0, "result": Row{"name": "研发", "parent_id": 0}})
		case "/topapi/v2/department/listsub":
			send(w, 200, Row{"errcode": 0, "result": []Row{}})
		case "/topapi/v2/user/list":
			people := []Row{{"userid": "emp-1", "unionid": "union-1", "name": "同名", "job_number": "001"}, {"userid": "emp-2", "unionid": "union-2", "name": "同名", "job_number": "002"}}
			if omitEmployee {
				people = people[1:]
			}
			send(w, 200, Row{"errcode": 0, "result": Row{"list": people, "has_more": false}})
		case "/topapi/v2/user/getuserinfo":
			if r.URL.Query().Get("access_token") != "app-token" || body(r)["code"] != "authorization-code" {
				t.Error("H5 code must be exchanged with the enterprise app token")
			}
			send(w, 200, Row{"errcode": 0, "result": Row{"userid": "emp-1", "unionid": "union-1"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mock.Close()
	a.Config.DingID = "client"
	a.Config.DingSecret = "secret"
	a.Config.DingCorp = "corp"
	a.Config.DingAPI = mock.URL
	a.Config.DingOAPI = mock.URL
	c.call("PUT", "/api/directory/settings", Row{"rootDepartmentIds": []int{1}, "intervalMinutes": 30}, 200)
	result := c.call("POST", "/api/directory/sync", Row{}, 200)
	if num(result["employees"]) != 2 {
		t.Fatal(result)
	}
	first := one(ctx, a.DB, "SELECT * FROM employees WHERE dingtalk_user_id='emp-1'")
	second := one(ctx, a.DB, "SELECT * FROM employees WHERE dingtalk_user_id='emp-2'")
	seed(ctx, a.DB, "ZC-201", "在用")
	seed(ctx, a.DB, "ZC-202", "在用")
	seed(ctx, a.DB, "ZC-203", "空闲")
	exec(ctx, a.DB, "UPDATE assets SET assignee='同名',assignee_employee_id=?,department='研发' WHERE asset_code='ZC-201'", first["id"])
	exec(ctx, a.DB, "UPDATE assets SET assignee='同名',assignee_employee_id=?,department='研发' WHERE asset_code='ZC-202'", second["id"])
	anon := testClient{t: t, handler: a.Handler()}
	login := anon.call("POST", "/api/auth/dingtalk/login", Row{"code": "authorization-code"}, 200)
	employee := testClient{t: t, handler: a.Handler(), token: str(login["token"])}
	mine := employee.call("GET", "/api/assets?scope=mine", nil, 200)
	if num(mine["total"]) != 1 {
		t.Fatal("same-name isolation", mine)
	}
	employee.call("GET", "/api/assets/ZC-202", nil, 404)
	employee.call("GET", "/api/assets/ZC-203", nil, 404)
	employee.call("GET", "/api/inventory/tasks", nil, 403)
	employee.call("GET", "/api/directory/status", nil, 403)
	failUpstream = true
	c.call("POST", "/api/directory/sync", Row{}, 502)
	if n := num(one(ctx, a.DB, "SELECT COUNT(*) AS n FROM employees")["n"]); n != 2 {
		t.Fatal("lost employees on failed sync")
	}
	failUpstream = false
	omitEmployee = true
	c.call("POST", "/api/directory/sync", Row{}, 200)
	employee.call("GET", "/api/auth/me", nil, 401)
	if one(ctx, a.DB, "SELECT missing_since FROM employees WHERE id=?", first["id"])["missing_since"] == nil {
		t.Fatal("missing employee not marked")
	}
	status := c.call("GET", "/api/directory/status", nil, 200)
	if num(status["employeesCount"]) != 2 {
		t.Fatal(status)
	}
}

func TestMySQLUserSessionRevocation(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	created := c.call("POST", "/api/auth/users", Row{"username": "other", "password": "  pass word  ", "display_name": "其他", "role": "viewer"}, 201)["user"].(map[string]any)
	anon := testClient{t: t, handler: a.Handler()}
	login := anon.call("POST", "/api/auth/login", Row{"username": "other", "password": "  pass word  "}, 200)
	other := testClient{t: t, handler: a.Handler(), token: str(login["token"])}
	other.call("GET", "/api/auth/me", nil, 200)
	c.call("PATCH", fmt.Sprintf("/api/auth/users/%d", num(created["id"])), Row{"role": "employee", "active": true}, 200)
	other.call("GET", "/api/auth/me", nil, 401)
	expired := time.Now().Add(-time.Hour).UnixMilli()
	exec(ctx, a.DB, "UPDATE sessions SET expires_at=?", expired)
	c.call("GET", "/api/assets", nil, 401)
}

func TestMySQLImportOwnershipExpiryAndGating(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	seed(ctx, a.DB, "ZC-301", "在用")
	p := upload(t, c, [][]any{{"资产编码", "资产名称", "资产状态"}, {"ZC-301", "变更", "空闲"}})
	if num(p["preview"].(map[string]any)["invalidRows"]) != 1 {
		t.Fatal("ownership bypass", p)
	}
	c.call("POST", "/api/imports/confirm", Row{"batchId": p["batchId"]}, 200)
	p = upload(t, c, [][]any{{"资产编码", "资产名称"}, {"ZC-302", "新增"}})
	exec(ctx, a.DB, "UPDATE import_previews SET expires_at=0 WHERE id=?", p["batchId"])
	c.call("POST", "/api/imports/confirm", Row{"batchId": p["batchId"]}, 409)
	a.heavy <- struct{}{}
	c.call("GET", "/api/exports/assets", nil, 429)
	<-a.heavy
}

func TestLegacyXLS(t *testing.T) {
	f, e := os.Open("testdata/legacy.xls")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	book := readWorkbook(f)
	defer book.Close()
	rows := parseWorkbook(book)
	if len(rows) != 1 || rows[0]["asset_code"] != "ZC-401" || rows[0]["name"] != "旧版电脑" {
		t.Fatal(rows)
	}
}
