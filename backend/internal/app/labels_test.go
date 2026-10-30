package app

import (
	"context"
	"net/http/httptest"
	"testing"
)

func labelInput(name string) Row {
	return Row{"name": name, "width": 50, "height": 30, "qrSize": 22, "padding": 2, "fontSize": 7, "title": "", "fields": []string{"name", "asset_code"}}
}
func TestMySQLLabels(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	path := "/api/labels/templates"
	initial := c.call("GET", path, nil, 200)["templates"].([]any)
	if len(initial) != 1 || num(initial[0].(map[string]any)["width"]) != 50 {
		t.Fatal(initial)
	}
	if exists(ctx, a.DB, "SELECT `key` FROM system_settings WHERE `key`=?", labelSettingsKey) {
		t.Fatal("read initialized database")
	}
	seed(ctx, a.DB, "LBL-001", "在用")
	seed(ctx, a.DB, "LBL-002", "空闲")
	exec(ctx, a.DB, "INSERT INTO asset_create_requests(request_id,request_hash,actor_id,asset_id) SELECT 'new-label','test',?,id FROM assets WHERE asset_code='LBL-002'", one(ctx, a.DB, "SELECT id FROM users WHERE role='admin'")["id"])
	exec(ctx, a.DB, "UPDATE assets SET qr_raw='https://d.gongbeiyun.com/a/LBL-001',assignee='历史使用人',department='原部门' WHERE asset_code='LBL-001'")
	before := js(one(ctx, a.DB, "SELECT * FROM assets WHERE asset_code='LBL-001'"))
	got := c.call("POST", "/api/labels/assets", Row{"assetCodes": []string{"LBL-002", "LBL-001"}}, 200)["assets"].([]any)
	if len(got) != 2 || got[0].(map[string]any)["asset_code"] != "LBL-002" {
		t.Fatal("selection order", got)
	}
	if got[0].(map[string]any)["qr_mode"] != "native" || got[1].(map[string]any)["qr_mode"] != "legacy" || got[1].(map[string]any)["qr_raw"] != "https://d.gongbeiyun.com/a/LBL-001" {
		t.Fatal("asset provenance or historic QR lost", got)
	}
	if before != js(one(ctx, a.DB, "SELECT * FROM assets WHERE asset_code='LBL-001'")) {
		t.Fatal("printing mutated ledger")
	}
	c.call("POST", "/api/labels/assets", Row{"assetCodes": []string{"LBL-001", "LBL-001"}}, 400)
	c.call("POST", "/api/labels/assets", Row{"assetCodes": []string{"LBL-999"}}, 409)
	c.call("POST", "/api/labels/assets", Row{"assetCodes": []string{}}, 400)
	created := c.call("POST", path, labelInput("自定义"), 201)["template"].(map[string]any)
	id := str(created["id"])
	edit := labelInput("修改")
	edit["expectedVersion"] = 1
	c.call("PATCH", path+"/"+id, edit, 200)
	c.call("PATCH", path+"/"+id, edit, 409)
	c.call("POST", path, labelInput("修改"), 409)
	invalid := labelInput("无效尺寸")
	invalid["qrSize"] = 35
	c.call("POST", path, invalid, 400)
	invalid = labelInput("缺少编码")
	invalid["fields"] = []string{"name", "model"}
	c.call("POST", path, invalid, 400)
	invalid = labelInput("错误数字")
	invalid["width"] = "NaN"
	c.call("POST", path, invalid, 400)
	c.call("DELETE", path+"/"+id, Row{"expectedVersion": 1}, 409)
	c.call("DELETE", path+"/"+id, Row{"expectedVersion": 2}, 200)
	c.call("DELETE", path+"/standard", Row{"expectedVersion": 1}, 409)
	for _, role := range []string{"employee", "asset_manager", "viewer"} {
		c.call("POST", "/api/auth/users", Row{"username": "labels" + role, "password": "labels-user-pass", "display_name": "测试", "role": role}, 201)
		login := testClient{t: t, handler: a.Handler()}.call("POST", "/api/auth/login", Row{"username": "labels" + role, "password": "labels-user-pass"}, 200)
		client := testClient{t: t, handler: a.Handler(), token: str(login["token"])}
		status := 403
		if role == "asset_manager" {
			status = 200
		}
		client.call("GET", path, nil, status)
		client.call("POST", "/api/labels/assets", Row{"assetCodes": []string{"LBL-001"}}, status)
		client.call("POST", path, labelInput("越权"), 403)
		client.call("DELETE", path+"/standard", Row{"expectedVersion": 1}, 403)
	}
	if num(one(ctx, a.DB, "SELECT COUNT(*) n FROM audit_logs WHERE action IN ('label_template_save','label_template_delete')")["n"]) != 3 {
		t.Fatal("template history")
	}
}

func TestAssetLabelReturnPath(t *testing.T) {
	for _, path := range []string{"/a/LBL-001?next=https://evil.test", "/a/LBL-001#bad", "//evil.test/a/LBL-001", "/a/../auth/login", "/a/not-valid/code"} {
		if ssoReturnPath(path) != "/" {
			t.Fatal(path)
		}
	}
	if ssoReturnPath("/a/LBL-001") != "/a/LBL-001" {
		t.Fatal("asset return lost")
	}
	a, c := testApp(t)
	f := mockOIDC(t, a)
	state, cookie := oidcStart(t, a, f, "/auth/login?return=%2Fa%2FLBL-001")
	if result := oidcCallback(a, state, "good", cookie); result.Header().Get("Location") != "/a/LBL-001" {
		t.Fatal("OIDC asset destination", result.Header())
	}
	// The deep-link HTML contains no asset data and does not bypass ledger authorization.
	seed(context.Background(), a.DB, "LBL-001", "空闲")
	c.call("POST", "/api/auth/users", Row{"username": "labelreader", "password": "labels-user-pass", "display_name": "员工", "role": "employee"}, 201)
	login := testClient{t: t, handler: a.Handler()}.call("POST", "/api/auth/login", Row{"username": "labelreader", "password": "labels-user-pass"}, 200)
	client := testClient{t: t, handler: a.Handler(), token: str(login["token"])}
	client.call("GET", "/api/assets/LBL-001", nil, 404)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/a/<bad>", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
