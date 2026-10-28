package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func companyInput(code, name string, parent, dept any) Row {
	return Row{"code": code, "name": name, "parent_id": parent, "dingtalk_department_id": dept, "remark": "测试备注"}
}

func TestMySQLCompanies(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	root := c.call("POST", "/api/companies", companyInput("01", "集团", nil, nil), 201)["company"].(map[string]any)
	child := c.call("POST", "/api/companies", companyInput("0102", "子公司", root["id"], nil), 201)["company"].(map[string]any)
	if num(c.call("GET", "/api/companies", nil, 200)["companies"].([]any)[1].(map[string]any)["parent_id"]) != num(root["id"]) {
		t.Fatal("parent not saved")
	}
	c.call("POST", "/api/companies", companyInput("01", "重复编码", nil, nil), 409)
	c.call("POST", "/api/companies", companyInput("02", "子公司", nil, nil), 409)
	c.call("POST", "/api/companies", companyInput(" bad ", "编码", nil, nil), 201) // trimming is supported
	c.call("POST", "/api/companies", companyInput("不合法", "编码", nil, nil), 400)
	c.call("POST", "/api/companies", companyInput("04", "", "", nil), 400)
	c.call("POST", "/api/companies", companyInput("04", "父不存在", 999999, nil), 400)
	b := companyInput("01", "集团改名", child["id"], nil)
	b["expectedVersion"] = root["version"]
	c.call("PATCH", fmt.Sprintf("/api/companies/%d", num(root["id"])), b, 409)
	b["parent_id"] = nil
	seed(ctx, a.DB, "CO-1", "空闲")
	exec(ctx, a.DB, "UPDATE assets SET company='集团' WHERE asset_code='CO-1'")
	c.call("PATCH", fmt.Sprintf("/api/companies/%d", num(root["id"])), b, 200)
	c.call("PATCH", fmt.Sprintf("/api/companies/%d", num(root["id"])), b, 409)
	list := c.call("GET", "/api/companies", nil, 200)["companies"].([]any)
	for _, raw := range list {
		company := raw.(map[string]any)
		if num(company["id"]) == num(root["id"]) && num(company["assets_count"]) != 1 {
			t.Fatal("rename lost historical asset identity", company)
		}
	}
	c.call("POST", "/api/companies", companyInput("03", "集团", nil, nil), 409)
	if one(ctx, a.DB, "SELECT company FROM assets WHERE asset_code='CO-1'")["company"] != "集团" {
		t.Fatal("historical asset changed")
	}
	exec(ctx, a.DB, "INSERT INTO system_settings (`key`,value) VALUES ('companies.seeded','1')")
	if e := Migrate(ctx, a.DB); e != nil {
		t.Fatal(e)
	}
	if exists(ctx, a.DB, "SELECT id FROM companies WHERE name='集团'") {
		t.Fatal("renamed company resurrected")
	}
	c.call("POST", "/api/auth/users", Row{"username": "readcompany", "password": "read-company-pass", "display_name": "只读", "role": "viewer"}, 201)
	login := testClient{t: t, handler: a.Handler()}.call("POST", "/api/auth/login", Row{"username": "readcompany", "password": "read-company-pass"}, 200)
	viewer := testClient{t: t, handler: a.Handler(), token: str(login["token"])}
	viewer.call("GET", "/api/companies", nil, 403)
	viewer.call("POST", "/api/companies", companyInput("99", "越权", nil, nil), 403)
	viewer.call("PATCH", fmt.Sprintf("/api/companies/%d", num(root["id"])), b, 403)
	if num(one(ctx, a.DB, "SELECT COUNT(*) n FROM audit_logs WHERE action='company_save'")["n"]) != 4 {
		t.Fatal("missing audit")
	}
}

func TestMySQLOrganizationSync(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	var mode atomic.Int32
	var tokens atomic.Int32
	var expired atomic.Bool
	reached := make(chan struct{}, 1)
	release := make(chan struct{})
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			tokens.Add(1)
			send(w, 200, Row{"accessToken": "app-token", "expireIn": 7200})
			return
		}
		if mode.Load() == 3 {
			select {
			case reached <- struct{}{}:
			default:
			}
			<-release
		}
		if expired.CompareAndSwap(false, true) {
			send(w, 200, Row{"errcode": 40014})
			return
		}
		b := body(r)
		id := num(b["dept_id"])
		names := map[int64]string{1: "集团", 2: "子公司部门", 3: "研发", 4: "行政"}
		switch r.URL.Path {
		case "/topapi/v2/department/get":
			parent := int64(1)
			if id == 1 {
				parent = 0
			}
			if id == 3 {
				parent = 2
			}
			send(w, 200, Row{"errcode": 0, "result": Row{"name": names[id], "parent_id": parent}})
		case "/topapi/v2/department/listsub":
			children := []Row{}
			if id == 1 {
				children = []Row{{"dept_id": 2}, {"dept_id": 4}}
			}
			if id == 2 {
				children = []Row{{"dept_id": 3}}
			}
			send(w, 200, Row{"errcode": 0, "result": children})
		case "/topapi/v2/user/list":
			if mode.Load() == 1 && id == 3 {
				send(w, 200, Row{"errcode": 60011})
				return
			}
			list := []Row{}
			more := false
			cursor := int64(0)
			if id == 3 && mode.Load() != 2 {
				if num(b["cursor"]) == 0 {
					list = []Row{{"userid": "alice", "unionid": "union-alice", "name": "张三", "job_number": "A001", "title": "工程师"}}
					more = true
					cursor = 100
				} else {
					list = []Row{{"userid": "bob", "name": "李四", "job_number": "B002", "title": "负责人"}}
				}
			}
			if id == 4 {
				list = []Row{{"userid": "alice", "unionid": "union-alice", "name": "张三", "job_number": "A001", "title": "工程师"}}
			}
			if mode.Load() == 4 {
				send(w, 200, Row{"errcode": 0, "result": Row{"list": list}})
				return
			}
			send(w, 200, Row{"errcode": 0, "result": Row{"list": list, "has_more": more, "next_cursor": cursor}})
		default:
			t.Errorf("unknown route %s", r.URL.Path)
			send(w, 404, Row{})
		}
	}))
	defer mock.Close()
	a.Config.DingID = "client"
	a.Config.DingSecret = "secret"
	a.Config.DingCorp = "corp"
	a.Config.DingAPI = mock.URL
	a.Config.DingOAPI = mock.URL
	root := c.call("POST", "/api/companies", companyInput("01", "集团公司", nil, nil), 201)["company"].(map[string]any)
	child := c.call("POST", "/api/companies", companyInput("0102", "分公司", root["id"], nil), 201)["company"].(map[string]any)
	c.call("PUT", "/api/directory/settings", Row{"rootDepartmentIds": []int{1}, "intervalMinutes": 0, "defaultCompanyId": root["id"]}, 200)
	c.call("POST", "/api/directory/sync", Row{}, 200)
	if tokens.Load() != 2 {
		t.Fatal("expired token not refreshed", tokens.Load())
	}
	bind := companyInput("0102", "分公司", root["id"], 2)
	bind["expectedVersion"] = child["version"]
	c.call("PATCH", fmt.Sprintf("/api/companies/%d", num(child["id"])), bind, 200)
	list := c.call("GET", "/api/directory/employees?all=true", nil, 200)["employees"].([]any)
	if len(list) != 2 {
		t.Fatal("pagination or deduplication", list)
	}
	alice := one(ctx, a.DB, "SELECT * FROM employees WHERE dingtalk_user_id='alice'")
	if alice["company"] != "集团公司 / 分公司" || alice["department"] != "研发 / 行政" || alice["job_number"] != "A001" || alice["title"] != "工程师" {
		t.Fatal("memberships missing", alice)
	}
	if num(one(ctx, a.DB, "SELECT COUNT(*) n FROM employee_departments WHERE employee_id=?", alice["id"])["n"]) != 2 {
		t.Fatal("multi department lost")
	}
	snapshot := str(alice["synced_at"])
	mode.Store(1)
	c.call("POST", "/api/directory/sync", Row{}, 502)
	if str(one(ctx, a.DB, "SELECT synced_at FROM employees WHERE id=?", alice["id"])["synced_at"]) != snapshot {
		t.Fatal("partial sync written")
	}
	status := c.call("GET", "/api/directory/status", nil, 200)
	if !strings.Contains(str(status["runs"].([]any)[0].(map[string]any)["message"]), "60011") {
		t.Fatal("upstream error not recorded", status)
	}
	mode.Store(4)
	c.call("POST", "/api/directory/sync", Row{}, 502)
	if exists(ctx, a.DB, "SELECT id FROM employees WHERE missing_since IS NOT NULL") {
		t.Fatal("malformed pagination deactivated people")
	}
	mode.Store(2)
	c.call("POST", "/api/directory/sync", Row{}, 200)
	if !exists(ctx, a.DB, "SELECT id FROM employees WHERE dingtalk_user_id='bob' AND missing_since IS NOT NULL") {
		t.Fatal("departed person not flagged")
	}
	// Async endpoint reserves a run before replying and serializes all configuration edits.
	mode.Store(3)
	job := c.call("POST", "/api/directory/sync/start", Row{}, 202)
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("async task did not start")
	}
	c.call("POST", "/api/directory/sync/start", Row{}, 409)
	c.call("POST", "/api/companies", companyInput("99", "同步中", nil, nil), 409)
	c.call("PUT", "/api/directory/settings", Row{"rootDepartmentIds": []int{1}, "intervalMinutes": 0}, 409)
	if c.call("GET", "/api/directory/status", nil, 200)["syncing"] != true {
		t.Fatal("missing running flag")
	}
	mode.Store(0)
	close(release)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		s := one(ctx, a.DB, "SELECT status FROM directory_sync_runs WHERE id=?", job["runId"])
		if s["status"] == "completed" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("async sync did not complete")
}

func TestCompanyAncestorResolution(t *testing.T) {
	ds := map[int64]Row{1: {"parent_id": 0}, 2: {"parent_id": 1}, 3: {"parent_id": 2}}
	if got := resolveDepartmentCompany(3, ds, map[int64]int64{1: 10, 2: 20}, 30); got != 20 {
		t.Fatal(got)
	}
	if got := resolveDepartmentCompany(3, ds, map[int64]int64{}, 30); got != 30 {
		t.Fatal(got)
	}
	ds[1]["parent_id"] = 3
	if got := resolveDepartmentCompany(3, ds, map[int64]int64{}, 0); got != 0 {
		t.Fatal("cycle")
	}
}
