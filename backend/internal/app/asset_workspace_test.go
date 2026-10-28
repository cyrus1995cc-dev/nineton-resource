package app

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func workspaceExportCodes(t *testing.T, c testClient, query string) []string {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/exports/assets?"+query, nil)
	r.Header.Set("Authorization", "Bearer "+c.token)
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("export = %d: %s", w.Code, w.Body.String())
	}
	book, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	data, err := book.GetRows("资产清单")
	if err != nil {
		t.Fatal(err)
	}
	column := -1
	for i, title := range data[0] {
		if title == "资产编码" {
			column = i
		}
	}
	if column < 0 {
		t.Fatal("asset code column missing")
	}
	codes := []string{}
	for _, row := range data[1:] {
		codes = append(codes, row[column])
	}
	return codes
}

func TestMySQLAssetWorkspace(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	owner := insert(ctx, a.DB, "INSERT INTO employees (name,department,dingtalk_user_id) VALUES ('同名','研发','workspace-owner')")
	other := insert(ctx, a.DB, "INSERT INTO employees (name,department,dingtalk_user_id) VALUES ('同名','保密部门','workspace-other')")
	for i, status := range []string{"在用", "在用", "空闲", "空闲", "空闲", "空闲", "报废"} {
		code := fmt.Sprintf("WS-%03d", i+1)
		seed(ctx, a.DB, code, status)
		exec(ctx, a.DB, "UPDATE assets SET company='测试公司',category_path='设备 > 电脑',department='研发',model='M1',serial_number=?,updated_at_system='2026-10-28 00:00:00' WHERE asset_code=?", "SN-"+code, code)
	}
	exec(ctx, a.DB, "UPDATE assets SET assignee_employee_id=?,assignee='同名' WHERE asset_code='WS-001'", owner)
	exec(ctx, a.DB, "UPDATE assets SET assignee_employee_id=?,assignee='同名',department='保密部门',company='保密公司',location='保密位置' WHERE asset_code='WS-002'", other)
	exec(ctx, a.DB, "UPDATE assets SET company='其他公司',department='行政' WHERE asset_code='WS-006'")
	exec(ctx, a.DB, "UPDATE assets SET name='公司名称测试' WHERE asset_code='WS-005'")
	q := url.Values{"company": {"测试公司"}, "department": {"研发"}, "status": {"空闲"}, "category": {"设备 > 电脑"}, "location": {"办公室"}, "sort": {"asset_code"}, "direction": {"asc"}}
	list := c.call("GET", "/api/assets?"+q.Encode(), nil, 200)
	if num(list["total"]) != 3 {
		t.Fatal("combined filters", list)
	}
	codes := []string{}
	for _, item := range list["items"].([]any) {
		codes = append(codes, item.(map[string]any)["asset_code"].(string))
	}
	if !reflect.DeepEqual(codes, []string{"WS-003", "WS-004", "WS-005"}) {
		t.Fatal("ordering", codes)
	}
	if exported := workspaceExportCodes(t, c, q.Encode()); !reflect.DeepEqual(codes, exported) {
		t.Fatal("list/export mismatch", codes, exported)
	}
	selected := "code=WS-005&code=WS-003&sort=asset_code&direction=desc"
	if got := workspaceExportCodes(t, c, selected); !reflect.DeepEqual(got, []string{"WS-005", "WS-003"}) {
		t.Fatal("selected export", got)
	}
	for _, spec := range []struct {
		field, search string
		total         int64
	}{{"company", "测试公司", 5}, {"name", "测试公司", 0}, {"serial_number", "sn-ws-003", 1}, {"all", "保密公司", 1}} {
		query := url.Values{"searchField": {spec.field}, "search": {spec.search}}
		if got := num(c.call("GET", "/api/assets?"+query.Encode(), nil, 200)["total"]); got != spec.total {
			t.Fatalf("search %s = %d", spec.field, got)
		}
	}
	first := c.call("GET", "/api/assets?pageSize=2&page=1", nil, 200)["items"].([]any)
	second := c.call("GET", "/api/assets?pageSize=2&page=2", nil, 200)["items"].([]any)
	if first[0].(map[string]any)["asset_code"] != "WS-007" || second[0].(map[string]any)["asset_code"] != "WS-005" {
		t.Fatal("stable pagination", first, second)
	}
	for _, query := range []string{"sort=name%3BSELECT+1", "direction=desc%3BDROP", "search=x&searchField=unknown", "code=invalid", strings.Repeat("code=WS-001&", 101)} {
		c.call("GET", "/api/assets?"+query, nil, 400)
		c.call("GET", "/api/exports/assets?"+query, nil, 400)
	}
	user := c.call("POST", "/api/auth/users", Row{"username": "workspaceemployee", "password": "workspace-pass", "display_name": "同名", "role": "employee"}, 201)["user"].(map[string]any)
	exec(ctx, a.DB, "UPDATE users SET employee_id=? WHERE id=?", owner, user["id"])
	login := testClient{t: t, handler: a.Handler()}.call("POST", "/api/auth/login", Row{"username": "workspaceemployee", "password": "workspace-pass"}, 200)
	employee := testClient{t: t, handler: a.Handler(), token: str(login["token"])}
	for _, scope := range []struct {
		name  string
		total int64
	}{{"mine", 1}, {"available", 4}, {"", 1}} {
		options := employee.call("GET", "/api/assets/options?scope="+scope.name, nil, 200)
		if num(options["total"]) != scope.total {
			t.Fatal("scoped options", scope.name, options)
		}
		if strings.Contains(js(options), "保密") {
			t.Fatal("private filter options leaked", options)
		}
	}
	if got := workspaceExportCodes(t, employee, "code=WS-002"); len(got) != 0 {
		t.Fatal("selected export bypassed ownership", got)
	}
	if got := workspaceExportCodes(t, employee, "scope=mine&code=WS-001&code=WS-002"); !reflect.DeepEqual(got, []string{"WS-001"}) {
		t.Fatal("mine export", got)
	}
	if got := workspaceExportCodes(t, employee, "scope=available&code=WS-001&code=WS-003"); !reflect.DeepEqual(got, []string{"WS-003"}) {
		t.Fatal("available export", got)
	}
	options := c.call("GET", "/api/assets/options", nil, 200)
	if num(options["total"]) != 7 {
		t.Fatal("admin options", options)
	}
}
