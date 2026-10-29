package app

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type CardField struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

var prefixPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,31}$`)
var categoryCodePattern = regexp.MustCompile(`^[A-Z0-9]{1,32}$`)
var customFieldPattern = regexp.MustCompile(`^custom_[a-z][a-z0-9_]{0,48}$`)
var templateFields = map[string]string{"model": "型号", "serial_number": "设备序列号", "cpu": "CPU", "memory": "内存", "discrete_gpu": "独立显卡", "gpu_model": "显卡型号", "remark": "备注", "photo_url": "资产照片地址", "amount_taxed": "含税金额", "amount_untaxed": "不含税金额", "tax_amount": "税额"}

func fieldsFor(keys ...string) []CardField {
	out := []CardField{}
	for _, key := range keys {
		typ := "text"
		if contains(amountFields, key) {
			typ = "number"
		}
		out = append(out, CardField{Key: key, Label: templateFields[key], Type: typ})
	}
	return out
}

// One initialization transaction derives metadata without changing existing assets or codes.
func (a *App) ensureCatalogue(ctx context.Context) {
	if exists(ctx, a.DB, "SELECT `key` FROM system_settings WHERE `key`='catalogue.seeded' AND value='1'") {
		return
	}
	transaction(ctx, a.DB, func(tx *sql.Tx) bool {
		exec(ctx, tx, "INSERT INTO system_settings (`key`,value) VALUES ('catalogue.seeded','0') ON DUPLICATE KEY UPDATE `key`=`key`")
		if str(one(ctx, tx, "SELECT value FROM system_settings WHERE `key`='catalogue.seeded' FOR UPDATE")["value"]) == "1" {
			return true
		}
		cards := map[string]int64{}
		groups := []struct {
			name   string
			fields []CardField
		}{
			{"通用类资产卡片", fieldsFor("model", "serial_number", "remark", "photo_url", "amount_taxed")},
			{"电脑资产类", fieldsFor("model", "serial_number", "cpu", "memory", "discrete_gpu", "gpu_model", "remark", "photo_url", "amount_taxed")},
			{"办公设备", fieldsFor("model", "serial_number", "remark", "photo_url", "amount_taxed")},
			{"办公家具", fieldsFor("model", "remark", "photo_url", "amount_taxed")},
			{"办公电器", fieldsFor("model", "serial_number", "remark", "photo_url", "amount_taxed")},
		}
		for _, g := range groups {
			exec(ctx, tx, "INSERT INTO asset_card_types(name,fields_json) VALUES (?,?) ON DUPLICATE KEY UPDATE name=name", g.name, js(g.fields))
			cards[g.name] = num(one(ctx, tx, "SELECT id FROM asset_card_types WHERE name=?", g.name)["id"])
		}
		defaults := []struct{ code, name, card, children string }{
			{"01", "计算机和网络设备", "电脑资产类", "BJB:笔记本电脑,PT:普通电脑,CSJ:手机,PB:平板电脑,XSQ:显示器,YF:研发电脑,SJ:设计电脑,JJ:剪辑电脑"},
			{"02", "办公设备", "办公设备", "DYJ:打印机,DS:电视机,BQJ:标签机,QT:其他办公设备"},
			{"03", "办公家具", "办公家具", "BGZ:桌子,GXY:人体工学椅,WJG:文件柜,BXG:保险柜,GZ:其他柜"},
			{"04", "办公电器", "办公电器", "KT:空调,BX:冰箱冰柜,YSJ:饮水机,WBL:微波炉,ZBJ:制冰机"},
			{"05", "账号", "通用类资产卡片", "ZH:账号"},
		}
		for _, g := range defaults {
			exec(ctx, tx, "INSERT INTO asset_categories(code,name,path,card_type_id) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE code=code", g.code, g.name, g.name, cards[g.card])
			parent := one(ctx, tx, "SELECT id FROM asset_categories WHERE path=?", g.name)["id"]
			for _, item := range strings.Split(g.children, ",") {
				pair := strings.SplitN(item, ":", 2)
				exec(ctx, tx, "INSERT INTO asset_categories(code,name,path,parent_id,card_type_id) VALUES (?,?,?,?,?) ON DUPLICATE KEY UPDATE code=code", pair[0], pair[1], g.name+"/"+pair[1], parent, cards[g.card])
				ensureCodeRule(ctx, tx, pair[0])
			}
		}
		// Preserve additional imported category paths as selectable categories.
		for _, v := range rows(ctx, tx, "SELECT DISTINCT category_path FROM assets WHERE COALESCE(category_path,'')<>'' ORDER BY category_path") {
			path := str(v["category_path"])
			if exists(ctx, tx, "SELECT id FROM asset_categories WHERE path=?", path) {
				continue
			}
			parts := strings.SplitN(path, "/", 2)
			var parent any
			if len(parts) == 2 {
				p := one(ctx, tx, "SELECT id FROM asset_categories WHERE path=?", parts[0])
				if p == nil {
					id := insert(ctx, tx, "INSERT INTO asset_categories(code,name,path,card_type_id) VALUES (?,?,?,?)", fmt.Sprintf("CAT%d", num(one(ctx, tx, "SELECT COALESCE(MAX(id),0)+1 n FROM asset_categories")["n"])), parts[0], parts[0], cards["通用类资产卡片"])
					parent = id
				} else {
					parent = p["id"]
				}
			}
			code := fmt.Sprintf("CAT%d", num(one(ctx, tx, "SELECT COALESCE(MAX(id),0)+1 n FROM asset_categories")["n"]))
			exec(ctx, tx, "INSERT INTO asset_categories(code,name,path,parent_id,card_type_id) VALUES (?,?,?,?,?)", code, parts[len(parts)-1], path, parent, cards["通用类资产卡片"])
			ensureCodeRule(ctx, tx, code)
		}
		exec(ctx, tx, "UPDATE system_settings SET value='1' WHERE `key`='catalogue.seeded'")
		return true
	})
}

func ensureCodeRule(ctx context.Context, q DBTX, prefix string) {
	exec(ctx, q, "INSERT INTO asset_code_rules(prefix) VALUES (?) ON DUPLICATE KEY UPDATE prefix=prefix", prefix)
}

func nextCode(ctx context.Context, q DBTX, rule Row) (string, int64) {
	next := num(rule["next_number"])
	prefix := str(rule["prefix"])
	// Include every existing numeric suffix, even if it was entered manually or imported.
	max := num(one(ctx, q, "SELECT COALESCE(MAX(CAST(SUBSTRING_INDEX(asset_code,'-',-1) AS UNSIGNED)),0) n FROM assets WHERE asset_code REGEXP ?", "^"+prefix+"-[0-9]{1,12}$")["n"])
	if next <= max {
		next = max + 1
	}
	if next > 999999999999 {
		fail(409, "该前缀的编号已用尽，请配置新前缀")
	}
	return fmt.Sprintf("%s-%0*d", prefix, int(num(rule["digits"])), next), next
}

func catalogueRows(ctx context.Context, q DBTX) Row {
	cards := rows(ctx, q, "SELECT * FROM asset_card_types ORDER BY id")
	for _, c := range cards {
		fields := []CardField{}
		decode(str(c["fields_json"]), &fields)
		c["fields"] = fields
		delete(c, "fields_json")
	}
	rules := rows(ctx, q, "SELECT * FROM asset_code_rules ORDER BY prefix")
	for _, rule := range rules {
		_, n := nextCode(ctx, q, rule)
		rule["next_number"] = n
	}
	return Row{"cards": cards, "categories": rows(ctx, q, "SELECT c.*,t.name card_name,(SELECT COUNT(*) FROM assets a WHERE a.category_path=c.path OR LEFT(a.category_path,CHAR_LENGTH(c.path)+1)=CONCAT(c.path,'/')) assets_count FROM asset_categories c JOIN asset_card_types t ON t.id=c.card_type_id ORDER BY COALESCE(c.parent_id,c.id),c.parent_id,c.id"), "rules": rules}
}
func (a *App) catalogue(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	a.ensureCatalogue(r.Context())
	send(w, 200, catalogueRows(r.Context(), a.DB))
}
func parseCardFields(value any) []CardField {
	raw, ok := value.([]any)
	if !ok || len(raw) > 40 {
		fail(400, "卡片最多配置40个字段")
	}
	fields := []CardField{}
	seen := map[string]bool{}
	for _, v := range raw {
		f, ok := v.(map[string]any)
		if !ok {
			fail(400, "卡片字段格式错误")
		}
		key := text(f["key"], 60, true)
		label := text(f["label"], 60, true)
		typ := text(f["type"], 20, true)
		if _, ok := templateFields[key]; !ok && !customFieldPattern.MatchString(key) {
			fail(400, "卡片字段编号无效")
		}
		if seen[key] || !contains([]string{"text", "number", "date", "select"}, typ) {
			fail(400, "卡片字段重复或类型无效")
		}
		seen[key] = true
		if contains(amountFields, key) && typ != "number" {
			fail(400, "金额字段必须使用数字类型")
		}
		if _, standard := templateFields[key]; standard && !contains(amountFields, key) && typ != "text" && typ != "select" {
			fail(400, "标准字段请使用文本或选项类型")
		}
		opts := []string{}
		if typ == "select" {
			values, ok := f["options"].([]any)
			if !ok || len(values) < 1 || len(values) > 50 {
				fail(400, "选项字段需配置1至50个选项")
			}
			for _, o := range values {
				t := text(o, 100, true)
				if !contains(opts, t) {
					opts = append(opts, t)
				}
			}
		}
		required, _ := f["required"].(bool)
		fields = append(fields, CardField{Key: key, Label: label, Type: typ, Required: required, Options: opts})
	}
	return fields
}
func boolInt(v any) int {
	if v == true || num(v) == 1 {
		return 1
	}
	return 0
}

func (a *App) saveCard(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	a.ensureCatalogue(r.Context())
	b := body(r)
	name := text(b["name"], 200, true)
	fields := parseCardFields(b["fields"])
	active := boolInt(b["active"])
	id := int64(0)
	if r.Method == "PATCH" {
		id = routeID(r, "id")
	}
	result := transaction(r.Context(), a.DB, func(tx *sql.Tx) Row {
		ctx := r.Context()
		old := one(ctx, tx, "SELECT * FROM asset_card_types WHERE id=? FOR UPDATE", id)
		if id > 0 && (old == nil || num(old["version"]) != integer(b["expectedVersion"], 0, 9007199254740991)) {
			fail(409, "卡片已被修改，请刷新后重试")
		}
		if active == 0 && exists(ctx, tx, "SELECT id FROM asset_categories WHERE card_type_id=? AND active=1", id) {
			fail(409, "请先将启用分类改绑其他卡片，再停用该卡片")
		}
		if id == 0 {
			id = insert(ctx, tx, "INSERT INTO asset_card_types(name,fields_json,active,updated_by) VALUES (?,?,?,?)", name, js(fields), active, actor(r)["display_name"])
		} else {
			exec(ctx, tx, "UPDATE asset_card_types SET name=?,fields_json=?,active=?,updated_by=?,updated_at=CURRENT_TIMESTAMP(3),version=version+1 WHERE id=?", name, js(fields), active, actor(r)["display_name"], id)
		}
		audit(ctx, tx, actor(r), "asset_card_save", str(id), Row{"name": name, "fields": fields, "active": active})
		return Row{"id": id}
	})
	send(w, 200, result)
}

func (a *App) saveCategory(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	a.ensureCatalogue(r.Context())
	b := body(r)
	name := text(b["name"], 100, true)
	code := text(b["code"], 32, true)
	remark := text(b["remark"], 1000, false)
	if strings.Contains(name, "/") || !categoryCodePattern.MatchString(code) {
		fail(400, "分类名称不能含斜杠，编码请使用大写字母和数字")
	}
	card := integer(b["card_type_id"], 0, 9007199254740991)
	var parent any
	if b["parent_id"] != nil {
		parent = integer(b["parent_id"], 0, 9007199254740991)
	}
	active := boolInt(b["active"])
	id := int64(0)
	if r.Method == "PATCH" {
		id = routeID(r, "id")
	}
	result := transaction(r.Context(), a.DB, func(tx *sql.Tx) Row {
		ctx := r.Context()
		all := rows(ctx, tx, "SELECT * FROM asset_categories ORDER BY id FOR UPDATE")
		byID := map[int64]Row{}
		for _, c := range all {
			byID[num(c["id"])] = c
		}
		old := byID[id]
		if id > 0 && (old == nil || num(old["version"]) != integer(b["expectedVersion"], 0, 9007199254740991)) {
			fail(409, "分类已被修改，请刷新后重试")
		}
		if !exists(ctx, tx, "SELECT id FROM asset_card_types WHERE id=? AND active=1 FOR SHARE", card) {
			fail(400, "请选择启用的资产卡片")
		}
		path := name
		if parent != nil {
			p := byID[num(parent)]
			if p == nil || p["parent_id"] != nil || num(parent) == id {
				fail(400, "上级分类必须是其他一级分类")
			}
			if num(p["active"]) == 0 && active == 1 {
				fail(400, "请先启用上级分类")
			}
			if !prefixPattern.MatchString(code) {
				fail(400, "子分类编码须以大写字母开头")
			}
			path = str(p["path"]) + "/" + name
		}
		if len([]rune(path)) > 200 {
			fail(400, "分类完整路径最多200字")
		}
		if old != nil && parent != nil && exists(ctx, tx, "SELECT id FROM asset_categories WHERE parent_id=?", id) {
			fail(409, "有下级分类，不能改为子分类")
		}
		if active == 0 && exists(ctx, tx, "SELECT id FROM asset_categories WHERE parent_id=? AND active=1", id) {
			fail(409, "请先停用下级分类")
		}
		if id == 0 {
			id = insert(ctx, tx, "INSERT INTO asset_categories(code,name,path,parent_id,card_type_id,active,remark) VALUES (?,?,?,?,?,?,?)", code, name, path, parent, card, active, remark)
		} else {
			exec(ctx, tx, "UPDATE asset_categories SET code=?,name=?,path=?,parent_id=?,card_type_id=?,active=?,remark=?,version=version+1 WHERE id=?", code, name, path, parent, card, active, remark, id)
			if str(old["path"]) != path {
				affected := []Row{old}
				for _, c := range all {
					if num(c["parent_id"]) == id {
						affected = append(affected, c)
					}
				}
				for _, c := range affected {
					newPath := path
					if num(c["id"]) != id {
						newPath = path + "/" + str(c["name"])
						if len([]rune(newPath)) > 200 {
							fail(400, "下级分类完整路径超过200字，请缩短分类名称")
						}
						exec(ctx, tx, "UPDATE asset_categories SET path=?,version=version+1 WHERE id=?", newPath, c["id"])
					}
					assets := rows(ctx, tx, "SELECT * FROM assets WHERE category_path=? FOR UPDATE", c["path"])
					exec(ctx, tx, "UPDATE assets SET category_path=?,category_level1=?,version=version+1,updated_at_system=CURRENT_TIMESTAMP(3) WHERE category_path=?", newPath, strings.Split(newPath, "/")[0], c["path"])
					for _, asset := range assets {
						writeEvent(ctx, tx, asset, "编辑", str(actor(r)["display_name"]), "资产分类调整", nil, nil, []Row{{"field": "category_path", "before": c["path"], "after": newPath}})
					}
				}
			}
		}
		if prefixPattern.MatchString(code) {
			ensureCodeRule(ctx, tx, code)
		}
		audit(ctx, tx, actor(r), "asset_category_save", str(id), b)
		return Row{"id": id}
	})
	send(w, 200, result)
}

func (a *App) saveCodeRule(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	a.ensureCatalogue(r.Context())
	b := body(r)
	prefix := text(b["prefix"], 32, true)
	if !prefixPattern.MatchString(prefix) {
		fail(400, "前缀须以大写字母开头，仅含大写字母和数字")
	}
	digits := integer(b["digits"], 0, 12)
	next := integer(b["next_number"], 0, 999999999999)
	ctx := r.Context()
	result := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		ensureCodeRule(ctx, tx, prefix)
		old := one(ctx, tx, "SELECT * FROM asset_code_rules WHERE prefix=? FOR UPDATE", prefix)
		if b["expectedVersion"] != nil && num(old["version"]) != integer(b["expectedVersion"], 0, 9007199254740991) {
			fail(409, "编码规则已被修改，请刷新后重试")
		}
		if next < num(old["next_number"]) {
			fail(409, "下一个流水号不能低于已分配的流水号")
		}
		exec(ctx, tx, "UPDATE asset_code_rules SET digits=?,next_number=?,version=version+1 WHERE prefix=?", digits, next, prefix)
		rule := one(ctx, tx, "SELECT * FROM asset_code_rules WHERE prefix=?", prefix)
		preview, n := nextCode(ctx, tx, rule)
		if n != next {
			exec(ctx, tx, "UPDATE asset_code_rules SET next_number=? WHERE prefix=?", n, prefix)
		}
		audit(ctx, tx, actor(r), "asset_code_rule_save", prefix, b)
		return Row{"preview": preview}
	})
	send(w, 200, result)
}

func (a *App) previewAssetCode(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	a.ensureCatalogue(r.Context())
	prefix := text(r.URL.Query().Get("prefix"), 32, true)
	rule := one(r.Context(), a.DB, "SELECT * FROM asset_code_rules WHERE prefix=?", prefix)
	if rule == nil {
		fail(404, "编码规则不存在")
	}
	code, _ := nextCode(r.Context(), a.DB, rule)
	send(w, 200, Row{"code": code})
}

func validateCustom(field CardField, v any) any {
	if field.Type == "number" {
		if v == nil || v == "" {
			if field.Required {
				fail(400, field.Label+"不能为空")
			}
			return nil
		}
		n, e := strconv.ParseFloat(str(v), 64)
		if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n > 1e12 || n < -1e12 {
			fail(400, field.Label+"请输入有效数字")
		}
		return n
	}
	value := text(v, 2000, field.Required)
	if field.Type == "select" && value != "" && !contains(field.Options, value) {
		fail(400, field.Label+"选项无效")
	}
	if field.Type == "date" && value != "" {
		if _, err := time.Parse("2006-01-02", value); err != nil {
			fail(400, field.Label+"请输入有效日期（YYYY-MM-DD）")
		}
	}
	return value
}
