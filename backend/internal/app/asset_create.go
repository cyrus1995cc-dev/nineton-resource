package app

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"
)

func (a *App) assetCompanies(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	send(w, 200, Row{"companies": rows(r.Context(), a.DB, "SELECT id,name FROM companies ORDER BY code,id")})
}

func assetCategory(ctx context.Context, q DBTX, id any) Row {
	category := one(ctx, q, "SELECT c.*,t.fields_json,t.active card_active FROM asset_categories c JOIN asset_card_types t ON t.id=c.card_type_id WHERE c.id=? FOR SHARE", id)
	if category == nil || num(category["active"]) != 1 || num(category["card_active"]) != 1 {
		fail(400, "请选择启用的资产分类和卡片")
	}
	if category["parent_id"] != nil && !exists(ctx, q, "SELECT id FROM asset_categories WHERE id=? AND active=1", category["parent_id"]) {
		fail(400, "上级分类已停用")
	}
	return category
}

func cardValues(category Row, b Row, previous Row, creating bool) Row {
	fields := []CardField{}
	decode(str(category["fields_json"]), &fields)
	out := Row{}
	if previous != nil {
		for k, v := range previous {
			out[k] = v
		}
	}
	input := Row{}
	if value, ok := b["custom_values"]; ok {
		m, ok := value.(map[string]any)
		if !ok {
			fail(400, "自定义字段格式无效")
		}
		input = m
	}
	known := map[string]bool{}
	for _, f := range fields {
		if strings.HasPrefix(f.Key, "custom_") {
			known[f.Key] = true
			v, ok := input[f.Key]
			if !ok {
				v = out[f.Key]
			}
			out[f.Key] = validateCustom(f, v)
			continue
		}
		v, ok := b[f.Key]
		if !ok && !creating {
			continue
		}
		if v == nil {
			v = ""
			if contains(amountFields, f.Key) {
				v = 0.0
			}
		}
		if issue := fieldProblem(f.Key, v); issue != "" {
			fail(400, issue)
		}
		if f.Required && !contains(amountFields, f.Key) && strings.TrimSpace(str(v)) == "" {
			fail(400, f.Label+"不能为空")
		}
		if f.Type == "select" && str(v) != "" && !contains(f.Options, str(v)) {
			fail(400, f.Label+"选项无效")
		}
	}
	for key := range input {
		if !known[key] {
			fail(400, "当前卡片未配置该自定义字段")
		}
	}
	return out
}

func saveProfile(ctx context.Context, q DBTX, assetID any, category Row, custom Row) {
	exec(ctx, q, "INSERT INTO asset_profiles(asset_id,category_id,card_type_id,custom_json) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE category_id=?,card_type_id=?,custom_json=?", assetID, category["id"], category["card_type_id"], js(custom), category["id"], category["card_type_id"], js(custom))
}

func (a *App) createAsset(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	ctx := r.Context()
	a.ensureCatalogue(ctx)
	b := body(r)
	requestID := text(b["requestId"], 100, true)
	digest := hash(js(Row{"actor": actor(r)["id"], "input": b}))
	categoryID := integer(b["category_id"], 0, 9007199254740991)
	mode := text(b["code_mode"], 20, false)
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" || text(b["asset_code"], 200, false) != "" {
		fail(400, "资产编码由所选分类自动生成，不能手动指定")
	}
	replayed := false
	result := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		// Serialize retries by this account before assigning a new sequence number.
		one(ctx, tx, "SELECT id FROM users WHERE id=? FOR UPDATE", actor(r)["id"])
		if old := one(ctx, tx, "SELECT * FROM asset_create_requests WHERE request_id=?", requestID); old != nil {
			if num(old["actor_id"]) != num(actor(r)["id"]) || str(old["request_hash"]) != digest {
				fail(409, "请求标识已用于其他内容，请重新打开新增窗口")
			}
			replayed = true
			code := str(one(ctx, tx, "SELECT asset_code FROM assets WHERE id=?", old["asset_id"])["asset_code"])
			return Row{"asset": assetDetail(ctx, tx, code), "replayed": true}
		}
		category := assetCategory(ctx, tx, categoryID)
		custom := cardValues(category, b, nil, true)
		prefix := str(category["code"])
		if !prefixPattern.MatchString(prefix) {
			fail(400, "请选择已设置字母编码前缀的资产分类")
		}
		ensureCodeRule(ctx, tx, prefix)
		rule := one(ctx, tx, "SELECT * FROM asset_code_rules WHERE prefix=? FOR UPDATE", prefix)
		code, n := nextCode(ctx, tx, rule)
		exec(ctx, tx, "UPDATE asset_code_rules SET next_number=?,version=version+1 WHERE id=?", n+1, rule["id"])
		if !codePattern.MatchString(code) {
			fail(400, "资产编码须为大写字母/数字前缀、短横线和编号")
		}
		if exists(ctx, tx, "SELECT id FROM assets WHERE asset_code=?", code) {
			fail(409, "资产编码已存在，请更换编码")
		}
		values := Row{"asset_code": code, "name": text(b["name"], 200, true), "category_path": category["path"], "category_level1": strings.Split(str(category["path"]), "/")[0], "status": "空闲", "created_at": time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05")}
		for _, key := range editFields {
			if contains([]string{"name", "category_path", "category_level1", "assignee", "department"}, key) {
				continue
			}
			v, ok := b[key]
			if !ok {
				continue
			}
			if issue := fieldProblem(key, v); issue != "" {
				fail(400, issue)
			}
			values[key] = v
		}
		// A company is selected by ID, and its canonical name is stored in the existing ledger.
		if b["company_id"] != nil {
			company := one(ctx, tx, "SELECT name FROM companies WHERE id=? FOR SHARE", integer(b["company_id"], 0, 9007199254740991))
			if company == nil {
				fail(400, "所选公司不存在")
			}
			values["company"] = company["name"]
		} else {
			values["company"] = ""
		}
		if b["assignee_employee_id"] != nil {
			employee := one(ctx, tx, "SELECT * FROM employees WHERE id=? AND active=1 AND missing_since IS NULL FOR SHARE", integer(b["assignee_employee_id"], 0, 9007199254740991))
			if employee == nil {
				fail(400, "请选择有效的通讯录员工")
			}
			values["assignee_employee_id"] = employee["id"]
			values["assignee"] = employee["name"]
			values["department"] = str(employee["department"])
			values["status"] = "在用"
		} else {
			if text(b["assignee"], 200, false) != "" || text(b["department"], 200, false) != "" {
				fail(400, "使用人和部门请从通讯录选择")
			}
			values["assignee"] = ""
			values["department"] = ""
			values["received_at"] = ""
		}
		keys := []string{}
		args := []any{}
		marks := []string{}
		for k, v := range values {
			keys = append(keys, "`"+k+"`")
			args = append(args, v)
			marks = append(marks, "?")
		}
		id := insert(ctx, tx, "INSERT INTO assets("+strings.Join(keys, ",")+") VALUES ("+strings.Join(marks, ",")+")", args...)
		saveProfile(ctx, tx, id, category, custom)
		changes := []Row{}
		for k, v := range values {
			if str(v) != "" {
				changes = append(changes, Row{"field": k, "before": "", "after": v})
			}
		}
		for k, v := range custom {
			if str(v) != "" {
				changes = append(changes, Row{"field": k, "before": "", "after": v})
			}
		}
		writeEvent(ctx, tx, Row{"id": id, "asset_code": code}, "入库", str(actor(r)["display_name"]), "新增资产入库", nil, nil, changes)
		exec(ctx, tx, "INSERT INTO asset_create_requests(request_id,request_hash,actor_id,asset_id) VALUES (?,?,?,?)", requestID, digest, actor(r)["id"], id)
		audit(ctx, tx, actor(r), "asset_create", code, Row{"category_id": categoryID, "code_mode": mode, "assignee_employee_id": values["assignee_employee_id"]})
		return Row{"asset": assetDetail(ctx, tx, code)}
	})
	status := 201
	if replayed {
		status = 200
	}
	send(w, status, result)
}

func attachAssetProfile(ctx context.Context, q DBTX, asset Row) {
	custom := Row{}
	profile := one(ctx, q, "SELECT * FROM asset_profiles WHERE asset_id=?", asset["id"])
	category := one(ctx, q, "SELECT * FROM asset_categories WHERE path=?", asset["category_path"])
	if profile != nil {
		decode(str(profile["custom_json"]), &custom)
	}
	asset["custom_values"] = custom
	if category == nil {
		return
	}
	asset["category_id"] = category["id"]
	card := one(ctx, q, "SELECT id,name,fields_json FROM asset_card_types WHERE id=?", category["card_type_id"])
	if card != nil {
		fields := []CardField{}
		decode(str(card["fields_json"]), &fields)
		asset["card_type"] = Row{"id": card["id"], "name": card["name"], "fields": fields}
	}
}
