package app

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestAssetCreateAndCatalogue(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	category := one(ctx, a.DB, "SELECT * FROM asset_categories WHERE code='CSJ'")
	input := func(id string) Row {
		return Row{"requestId": id, "category_id": category["id"], "code_mode": "auto", "name": "测试手机"}
	}
	seed(ctx, a.DB, "CSJ-0419", "空闲")
	exec(ctx, a.DB, "UPDATE assets SET qr_raw='https://d.gongbeiyun.com/a/CSJ-0419' WHERE asset_code='CSJ-0419'")
	for i := 0; i < 2; i++ {
		if v := c.call("GET", "/api/asset-catalogue/code-preview?prefix=CSJ", nil, 200)["code"]; v != "CSJ-0420" {
			t.Fatal("preview", v)
		}
	}
	bad := input("failed-does-not-allocate")
	bad["name"] = ""
	c.call("POST", "/api/assets", bad, 400)
	b := input("create-first")
	asset := c.call("POST", "/api/assets", b, 201)["asset"].(map[string]any)
	if asset["asset_code"] != "CSJ-0420" || asset["status"] != "空闲" || num(asset["category_id"]) != num(category["id"]) {
		t.Fatal(asset)
	}
	replay := c.call("POST", "/api/assets", b, 200)["asset"].(map[string]any)
	if replay["id"] != asset["id"] {
		t.Fatal("idempotency")
	}
	b["name"] = "different"
	c.call("POST", "/api/assets", b, 409)
	manual := input("manual-rejected")
	manual["code_mode"] = "manual"
	manual["asset_code"] = "CSJ-0419"
	c.call("POST", "/api/assets", manual, 400)
	manual["code_mode"] = "auto"
	c.call("POST", "/api/assets", manual, 400)
	seed(ctx, a.DB, "CSJ-1000", "空闲")
	if n := c.call("GET", "/api/asset-catalogue/code-preview?prefix=CSJ", nil, 200)["code"]; n != "CSJ-1001" {
		t.Fatal(n)
	}
	employee := insert(ctx, a.DB, "INSERT INTO employees(name,department,dingtalk_user_id) VALUES ('通讯录员工','萌宠事业部','catalogue-employee')")
	company := insert(ctx, a.DB, "INSERT INTO companies(code,name) VALUES ('TEST','测试公司')")
	assigned := input("assigned")
	assigned["assignee_employee_id"] = employee
	assigned["company_id"] = company
	assigned["received_at"] = "2026-10-29"
	assigned["assignee"] = "伪造名字"
	assigned["department"] = "伪造部门"
	v := c.call("POST", "/api/assets", assigned, 201)["asset"].(map[string]any)
	if v["assignee"] != "通讯录员工" || v["department"] != "萌宠事业部" || v["company"] != "测试公司" || v["status"] != "在用" {
		t.Fatal("canonical directory", v)
	}
	invalid := input("unknown-person")
	invalid["assignee"] = "手工员工"
	c.call("POST", "/api/assets", invalid, 400)
	exec(ctx, a.DB, "UPDATE employees SET active=0 WHERE id=?", employee)
	invalid["assignee_employee_id"] = employee
	c.call("POST", "/api/assets", invalid, 400)
	rule := one(ctx, a.DB, "SELECT * FROM asset_code_rules WHERE prefix='CSJ'")
	c.call("PUT", "/api/asset-catalogue/code-rules", Row{"prefix": "CSJ", "digits": 5, "next_number": 2000, "expectedVersion": rule["version"]}, 200)
	c.call("PUT", "/api/asset-catalogue/code-rules", Row{"prefix": "CSJ", "digits": 4, "next_number": 2000, "expectedVersion": rule["version"]}, 409)
	rule = one(ctx, a.DB, "SELECT * FROM asset_code_rules WHERE prefix='CSJ'")
	c.call("PUT", "/api/asset-catalogue/code-rules", Row{"prefix": "CSJ", "digits": 5, "next_number": 1, "expectedVersion": rule["version"]}, 409)
	if v := c.call("GET", "/api/asset-catalogue/code-preview?prefix=CSJ", nil, 200)["code"]; v != "CSJ-02000" {
		t.Fatal(v)
	}
	for _, role := range []string{"employee", "viewer", "asset_manager"} {
		c.call("POST", "/api/auth/users", Row{"username": role, "password": "catalogue-pass", "display_name": role, "role": role}, 201)
		anon := testClient{t: t, handler: a.Handler()}
		login := anon.call("POST", "/api/auth/login", Row{"username": role, "password": "catalogue-pass"}, 200)
		client := testClient{t: t, handler: a.Handler(), token: str(login["token"])}
		client.call("POST", "/api/asset-catalogue/cards", Row{}, 403)
		client.call("PUT", "/api/asset-catalogue/code-rules", Row{}, 403)
		if role == "asset_manager" {
			client.call("GET", "/api/asset-catalogue/companies", nil, 200)
			client.call("GET", "/api/companies", nil, 403)
			client.call("POST", "/api/assets", input("manager-create"), 201)
		} else {
			client.call("POST", "/api/assets", input(role), 403)
			client.call("GET", "/api/asset-catalogue", nil, 403)
		}
	}
	if v := c.call("GET", "/api/assets/CSJ-0419", nil, 200); v["qr_raw"] != "https://d.gongbeiyun.com/a/CSJ-0419" {
		t.Fatal("old QR changed")
	}
	if n := num(one(ctx, a.DB, "SELECT COUNT(*) n FROM asset_events WHERE asset_id=? AND type='入库'", asset["id"])["n"]); n != 1 {
		t.Fatal("duplicate entry event", n)
	}
}

func TestCardFieldsCategoriesAndEditing(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	fields := []Row{{"key": "model", "label": "型号", "type": "text", "required": true}, {"key": "custom_warranty", "label": "保修截止", "type": "date", "required": true}, {"key": "custom_count", "label": "配件数", "type": "number", "required": false}, {"key": "custom_system", "label": "系统", "type": "select", "required": true, "options": []string{"Android", "iOS"}}}
	card := c.call("POST", "/api/asset-catalogue/cards", Row{"name": "自定义手机卡片", "active": 1, "fields": fields}, 200)["id"]
	cat := c.call("POST", "/api/asset-catalogue/categories", Row{"code": "TEST", "name": "测试分类", "active": 1, "card_type_id": card}, 200)["id"]
	create := Row{"requestId": "custom-create", "category_id": cat, "code_mode": "auto", "name": "测试设备", "custom_values": Row{"custom_warranty": "2027-01-01", "custom_system": "Android"}}
	c.call("POST", "/api/assets", create, 400)
	create["model"] = "测试型号"
	create["custom_values"].(Row)["custom_system"] = "无效选项"
	c.call("POST", "/api/assets", create, 400)
	create["custom_values"].(Row)["custom_system"] = "Android"
	create["custom_values"].(Row)["custom_count"] = "NaN"
	c.call("POST", "/api/assets", create, 400)
	create["custom_values"].(Row)["custom_count"] = 2
	asset := c.call("POST", "/api/assets", create, 201)["asset"].(map[string]any)
	code := str(asset["asset_code"])
	c.call("PATCH", "/api/assets/"+code, Row{"expectedVersion": asset["version"], "category_id": cat, "model": ""}, 400)
	c.call("PATCH", "/api/assets/"+code, Row{"expectedVersion": asset["version"], "category_id": cat, "custom_values": Row{"custom_warranty": ""}}, 400)
	updated := c.call("PATCH", "/api/assets/"+code, Row{"expectedVersion": asset["version"], "category_id": cat, "custom_values": Row{"custom_count": 3}}, 200)["asset"].(map[string]any)
	custom := updated["custom_values"].(map[string]any)
	if num(custom["custom_count"]) != 3 || custom["custom_system"] != "Android" {
		t.Fatal(custom)
	}
	c.call("PATCH", "/api/assets/"+code, Row{"expectedVersion": updated["version"], "location": "绕过审批"}, 409)
	old := one(ctx, a.DB, "SELECT * FROM asset_categories WHERE id=?", cat)
	c.call("PATCH", fmt.Sprintf("/api/asset-catalogue/categories/%d", num(cat)), Row{"code": "TEST2", "name": "改名分类", "active": 1, "card_type_id": card, "expectedVersion": old["version"]}, 200)
	asset = c.call("GET", "/api/assets/"+code, nil, 200)
	if asset["category_path"] != "改名分类" || asset["asset_code"] != code {
		t.Fatal(asset)
	}
	c.call("PATCH", fmt.Sprintf("/api/asset-catalogue/categories/%d", num(cat)), Row{"code": "TEST2", "name": "改名分类", "active": 1, "card_type_id": card, "expectedVersion": old["version"]}, 409)
	oldCard := one(ctx, a.DB, "SELECT * FROM asset_card_types WHERE id=?", card)
	c.call("PATCH", fmt.Sprintf("/api/asset-catalogue/cards/%d", num(card)), Row{"name": "自定义手机卡片", "active": 0, "fields": fields, "expectedVersion": oldCard["version"]}, 409)
	old = one(ctx, a.DB, "SELECT * FROM asset_categories WHERE id=?", cat)
	c.call("PATCH", fmt.Sprintf("/api/asset-catalogue/categories/%d", num(cat)), Row{"code": "TEST2", "name": "改名分类", "active": 0, "card_type_id": card, "expectedVersion": old["version"]}, 200)
	create["requestId"] = "inactive-category"
	c.call("POST", "/api/assets", create, 400)
}

func TestConcurrentAssetCodes(t *testing.T) {
	a, c := testApp(t)
	ctx := context.Background()
	cat := one(ctx, a.DB, "SELECT id FROM asset_categories WHERE code='XSQ'")["id"]
	clients := []testClient{c}
	for i := 1; i < 4; i++ {
		name := fmt.Sprintf("manager%d", i)
		c.call("POST", "/api/auth/users", Row{"username": name, "password": "concurrent-pass", "display_name": name, "role": "asset_manager"}, 201)
		login := c.call("POST", "/api/auth/login", Row{"username": name, "password": "concurrent-pass"}, 200)
		clients = append(clients, testClient{t: t, handler: a.Handler(), token: str(login["token"])})
	}
	var wg sync.WaitGroup
	codes := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v := clients[i%len(clients)].call("POST", "/api/assets", Row{"requestId": fmt.Sprintf("parallel-%d", i), "category_id": cat, "name": "并发设备", "code_mode": "auto"}, 201)
			codes <- str(v["asset"].(map[string]any)["asset_code"])
		}(i)
	}
	wg.Wait()
	close(codes)
	seen := map[string]bool{}
	for code := range codes {
		if seen[code] {
			t.Fatal("duplicate", code)
		}
		seen[code] = true
	}
	if len(seen) != 12 {
		t.Fatal(len(seen))
	}
	if v := c.call("GET", "/api/asset-catalogue/code-preview?prefix=XSQ", nil, 200)["code"]; v != "XSQ-0013" {
		t.Fatal(v)
	}
}
