package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestPasswordCompatibility(t *testing.T) {
	encoded := PasswordHash("test password")
	if !VerifyPassword("test password", encoded) || VerifyPassword("wrong", encoded) || VerifyPassword("test password", "external:dingtalk") {
		t.Fatal("scrypt verification")
	}
}
func TestQR(t *testing.T) {
	for _, raw := range []string{"ZC-001", "https://d.gongbeiyun.com/a/ZC-001", "https://assets.example/a/ZC-001"} {
		code, _ := parseQR(raw)
		if code != "ZC-001" {
			t.Fatal(raw)
		}
	}
	if c, _ := parseQR("https://malicious.example/invalid"); c != "" {
		t.Fatal(c)
	}
}

type testClient struct {
	t       *testing.T
	handler http.Handler
	token   string
}

func (c testClient) call(method, path string, b any, status int) Row {
	c.t.Helper()
	var reader io.Reader
	if b != nil {
		reader = strings.NewReader(js(b))
	}
	r := httptest.NewRequest(method, path, reader)
	if c.token != "" {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != status {
		c.t.Fatalf("%s %s = %d, want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	if w.Body.Len() == 0 {
		return nil
	}
	out := Row{}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		c.t.Fatal(e)
	}
	return out
}
func testApp(t *testing.T) (*App, testClient) {
	t.Helper()
	dsn := os.Getenv("ASSET_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set ASSET_TEST_MYSQL_DSN to a dedicated empty MySQL 8.4 test database")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatal("test database name must contain _test")
	}
	db, e := Open(dsn, 8)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if e = Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec("SET FOREIGN_KEY_CHECKS=0")
	if e != nil {
		t.Fatal(e)
	}
	tables := append(Tables(), "sessions")
	for _, table := range tables {
		if _, e = tx.Exec("DELETE FROM `" + table + "`"); e != nil {
			t.Fatal(e)
		}
	}
	_, e = tx.Exec("SET FOREIGN_KEY_CHECKS=1")
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	a := New(db, Config{AdminPassword: "integration-password"})
	a.Bootstrap(ctx)
	c := testClient{t: t, handler: a.Handler()}
	login := c.call("POST", "/api/auth/login", Row{"username": "admin", "password": "integration-password"}, 200)
	c.token = str(login["token"])
	return a, c
}
func seed(ctx context.Context, db *sql.DB, code, status string) {
	exec(ctx, db, "INSERT INTO assets (asset_code,name,status,location,assignee,department) VALUES (?,? ,?,'办公室','','')", code, "电脑", status)
}
func TestMySQLCore(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	seed(ctx, a.DB, "ZC-001", "空闲")
	seed(ctx, a.DB, "ZC-002", "空闲")
	if total := num(c.call("GET", "/api/assets?search=zc-001", nil, 200)["total"]); total != 1 {
		t.Fatal("case-insensitive asset search", total)
	}
	t.Run("authentication and roles", func(t *testing.T) {
		anon := testClient{t: t, handler: a.Handler()}
		anon.call("GET", "/api/assets", nil, 401)
		c.call("POST", "/api/auth/users", Row{"username": "viewer", "password": "viewerpass", "display_name": "只读", "role": "viewer"}, 201)
		v := anon.call("POST", "/api/auth/login", Row{"username": "viewer", "password": "viewerpass"}, 200)
		viewer := testClient{t: t, handler: a.Handler(), token: str(v["token"])}
		viewer.call("GET", "/api/assets", nil, 200)
		viewer.call("POST", "/api/inventory/tasks", Row{"name": "违规"}, 403)
	})
	t.Run("optimistic editing and QR preservation", func(t *testing.T) {
		v := c.call("GET", "/api/assets/ZC-001", nil, 200)
		c.call("PATCH", "/api/assets/ZC-001", Row{"name": "新电脑", "expectedVersion": v["version"]}, 200)
		c.call("PATCH", "/api/assets/ZC-001", Row{"name": "覆盖", "expectedVersion": v["version"]}, 409)
		v = c.call("GET", "/api/assets/ZC-001", nil, 200)
		c.call("PATCH", "/api/assets/ZC-001", Row{"location": "仓库", "expectedVersion": v["version"]}, 409)
		old := "https://d.gongbeiyun.com/a/ZC-001"
		c.call("POST", "/api/assets/ZC-001/qr", Row{"raw": old}, 200)
		result := c.call("POST", "/api/assets/ZC-001/qr", Row{"raw": "ZC-001"}, 200)
		if result["qr_raw"] != old {
			t.Fatal("QR overwritten")
		}
		if n := num(one(ctx, a.DB, "SELECT COUNT(*) AS n FROM asset_qr_aliases")["n"]); n != 1 {
			t.Fatal(n)
		}
	})
	t.Run("ordered approvals and idempotence", func(t *testing.T) {
		for _, name := range []string{"approver1", "approver2"} {
			c.call("POST", "/api/auth/users", Row{"username": name, "password": "approvalpass", "display_name": name, "role": "approver"}, 201)
		}
		options := c.call("GET", "/api/flows/options", nil, 200)["approvers"].([]any)
		ids := []any{}
		for _, raw := range options {
			v := raw.(map[string]any)
			if strings.HasPrefix(str(v["username"]), "approver") {
				ids = append(ids, v["id"])
			}
		}
		b := Row{"type": "领用", "assetCodes": []string{"ZC-001", "ZC-002"}, "applicantDepartment": "行政", "approverIds": ids, "toAssignee": "张三", "toDepartment": "行政", "requestId": "flow-test-one"}
		detail := c.call("POST", "/api/flows", b, 201)
		id := num(detail["flow"].(map[string]any)["id"])
		c.call("POST", "/api/flows", b, 200)
		b["note"] = "different"
		c.call("POST", "/api/flows", b, 409)
		anon := testClient{t: t, handler: a.Handler()}
		clients := []testClient{}
		for _, name := range []string{"approver1", "approver2"} {
			login := anon.call("POST", "/api/auth/login", Row{"username": name, "password": "approvalpass"}, 200)
			clients = append(clients, testClient{t: t, handler: a.Handler(), token: str(login["token"])})
		}
		path := fmt.Sprintf("/api/flows/%d/approve", id)
		clients[1].call("POST", path, Row{}, 403)
		clients[0].call("POST", path, Row{}, 200)
		if v := c.call("GET", "/api/assets/ZC-001", nil, 200); v["status"] != "空闲" {
			t.Fatal("updated before final approval")
		}
		clients[1].call("POST", path, Row{}, 200)
		clients[1].call("POST", path, Row{}, 200)
		for _, code := range []string{"ZC-001", "ZC-002"} {
			v := c.call("GET", "/api/assets/"+code, nil, 200)
			if v["status"] != "在用" || v["assignee"] != "张三" {
				t.Fatal(v)
			}
		}
		if n := num(one(ctx, a.DB, "SELECT COUNT(*) AS n FROM asset_events WHERE flow_id=?", id)["n"]); n != 2 {
			t.Fatal("duplicate events", n)
		}
	})
	t.Run("inventory replay and completion", func(t *testing.T) {
		detail := c.call("POST", "/api/inventory/tasks", Row{"name": "盘点"}, 201)
		id := num(detail["task"].(map[string]any)["id"])
		path := fmt.Sprintf("/api/inventory/tasks/%d", id)
		scan := Row{"assetCode": "ZC-001", "actualLocation": "仓库", "note": "核对"}
		c.call("POST", path+"/records", scan, 200)
		v := c.call("POST", path+"/records", scan, 200)
		if v["replayed"] != true {
			t.Fatal(v)
		}
		v = c.call("POST", path+"/complete", Row{}, 200)
		task := v["task"].(map[string]any)
		if num(task["checked_count"]) != 1 || num(task["difference_count"]) != 2 {
			t.Fatal(task)
		}
		c.call("POST", path+"/records", scan, 409)
		if one(ctx, a.DB, "SELECT location FROM assets WHERE asset_code='ZC-001'")["location"] != "办公室" {
			t.Fatal("inventory changed location")
		}
		r := httptest.NewRequest("GET", fmt.Sprintf("/api/exports/inventory/%d", id), nil)
		r.Header.Set("Authorization", "Bearer "+c.token)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		book, e := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
		if e != nil {
			t.Fatal(e)
		}
		defer book.Close()
		if len(book.GetSheetList()) != 4 {
			t.Fatal(book.GetSheetList())
		}
	})
}
func upload(t *testing.T, c testClient, data [][]any) Row {
	t.Helper()
	book := excelize.NewFile()
	defer book.Close()
	for i, row := range data {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if e := book.SetSheetRow("Sheet1", cell, &row); e != nil {
			t.Fatal(e)
		}
	}
	buf, e := book.WriteToBuffer()
	if e != nil {
		t.Fatal(e)
	}
	var request bytes.Buffer
	form := multipart.NewWriter(&request)
	part, e := form.CreateFormFile("file", "assets.xlsx")
	if e != nil {
		t.Fatal(e)
	}
	_, e = part.Write(buf.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	form.Close()
	r := httptest.NewRequest("POST", "/api/imports/preview", &request)
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("preview %d: %s", w.Code, w.Body.String())
	}
	var out Row
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestMySQLImportAndExport(t *testing.T) {
	a, c := testApp(t)
	p := upload(t, c, [][]any{{"资产编码", "资产名称", "购置/租赁金额（含税）", "备注"}, {"ZC-001", "电脑", 100, "=1+1"}, {"ZC-001", "重复", 200, ""}, {"bad", "编码无效", 0, ""}})
	counts := p["preview"].(map[string]any)
	if num(counts["newRows"]) != 1 || num(counts["invalidRows"]) != 2 {
		t.Fatal(p)
	}
	confirm := Row{"batchId": p["batchId"]}
	c.call("POST", "/api/imports/confirm", confirm, 200)
	c.call("POST", "/api/imports/confirm", confirm, 200)
	p = upload(t, c, [][]any{{"资产编码", "资产名称", "购置/租赁金额（含税）"}, {"ZC-001", "更新", 0}})
	version := one(context.Background(), a.DB, "SELECT version FROM assets WHERE asset_code='ZC-001'")["version"]
	c.call("PATCH", "/api/assets/ZC-001", Row{"name": "外部修改", "expectedVersion": version}, 200)
	c.call("POST", "/api/imports/confirm", Row{"batchId": p["batchId"]}, 409)
	p = upload(t, c, [][]any{{"资产编码", "资产名称", "购置/租赁金额（含税）"}, {"ZC-001", "更新", 0}})
	c.call("POST", "/api/imports/confirm", Row{"batchId": p["batchId"]}, 200)
	if n := num(one(context.Background(), a.DB, "SELECT amount_taxed FROM assets WHERE asset_code='ZC-001'")["amount_taxed"]); n != 0 {
		t.Fatal(n)
	}
	r := httptest.NewRequest("GET", "/api/exports/assets", nil)
	r.Header.Set("Authorization", "Bearer "+c.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	book, e := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	defer book.Close()
	if formula, _ := book.GetCellFormula("资产清单", "O2"); formula != "" {
		t.Fatal("formula injection")
	}
	if s, _ := book.GetCellValue("资产清单", "O2"); s != "=1+1" {
		t.Fatal("literal lost", s)
	}
}
func TestMySQLConcurrentInventory(t *testing.T) {
	a, c := testApp(t)
	seed(context.Background(), a.DB, "ZC-001", "空闲")
	task := c.call("POST", "/api/inventory/tasks", Row{"name": "并发"}, 201)["task"].(map[string]any)
	path := fmt.Sprintf("/api/inventory/tasks/%d/records", num(task["id"]))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", path, strings.NewReader(`{"assetCode":"ZC-001"}`))
			r.Header.Set("Authorization", "Bearer "+c.token)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Errorf("concurrent scan %d: %s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	if n := num(one(context.Background(), a.DB, "SELECT COUNT(*) AS n FROM asset_events WHERE type='盘点'")["n"]); n != 1 {
		t.Fatal("duplicate scan history", n)
	}
}
