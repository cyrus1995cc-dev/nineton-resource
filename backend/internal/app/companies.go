package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var companyCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$`)

// Serialize configuration changes with sync startup. A sync reads a stable mapping.
func (a *App) directoryEdit() func() {
	a.syncMu.Lock()
	if a.syncing {
		a.syncMu.Unlock()
		fail(409, "通讯录正在同步，请完成后再修改公司或同步设置")
	}
	return a.syncMu.Unlock
}

func (a *App) companies(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	send(w, 200, Row{"companies": rows(r.Context(), a.DB, `SELECT c.*,p.name AS parent_name,
 (SELECT COUNT(DISTINCT ed.employee_id) FROM employee_departments ed JOIN employees e ON e.id=ed.employee_id WHERE ed.company_id=c.id AND e.active=1 AND e.missing_since IS NULL) AS employees_count,
 (SELECT COUNT(*) FROM assets a WHERE a.company IN (SELECT cn.name FROM company_names cn WHERE cn.company_id=c.id)) AS assets_count
 FROM companies c LEFT JOIN companies p ON p.id=c.parent_id ORDER BY c.code,c.id`)})
}

func (a *App) saveCompany(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	unlock := a.directoryEdit()
	defer unlock()
	b := body(r)
	ctx := r.Context()
	code := text(b["code"], 80, true)
	name := text(b["name"], 200, true)
	remark := text(b["remark"], 1000, false)
	if !companyCode.MatchString(code) {
		fail(400, "公司编码须为字母、数字、点、横线或下划线，且以字母或数字开头")
	}
	var parent, dept any
	if b["parent_id"] != nil {
		parent = integer(b["parent_id"], 0, 9007199254740991)
	}
	if b["dingtalk_department_id"] != nil {
		dept = integer(b["dingtalk_department_id"], 0, 9007199254740991)
	}
	id := int64(0)
	version := int64(0)
	if r.Method == "PATCH" {
		id = routeID(r, "id")
		version = integer(b["expectedVersion"], 0, 9007199254740991)
	}
	result := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		// Lock all companies to make concurrent hierarchy and uniqueness checks consistent.
		all := rows(ctx, tx, "SELECT * FROM companies ORDER BY id FOR UPDATE")
		byID := map[int64]Row{}
		var old Row
		for _, c := range all {
			byID[num(c["id"])] = c
			if num(c["id"]) == id {
				old = c
			}
			if num(c["id"]) != id && (strings.EqualFold(str(c["code"]), code) || strings.EqualFold(str(c["name"]), name)) {
				fail(409, "公司编码或公司名称已存在")
			}
		}
		if id != 0 && old == nil {
			fail(404, "公司不存在")
		}
		if old != nil && num(old["version"]) != version {
			fail(409, "公司信息已被修改，请刷新后重新编辑")
		}
		if exists(ctx, tx, "SELECT company_id FROM company_names WHERE name COLLATE utf8mb4_0900_as_ci = ? AND company_id<>?", name, id) {
			fail(409, "该名称已用于其他公司的历史记录，请使用不同名称")
		}
		seen := map[int64]bool{}
		for next := num(parent); next != 0; {
			if next == id || seen[next] {
				fail(409, "上级公司不能选择本公司或其下级公司")
			}
			seen[next] = true
			p := byID[next]
			if p == nil {
				fail(400, "上级公司不存在")
			}
			next = num(p["parent_id"])
		}
		var corp any
		if dept != nil {
			if !a.dingConfigured() {
				fail(409, "请先配置钉钉应用并同步部门，再绑定公司")
			}
			if !exists(ctx, tx, "SELECT id FROM departments WHERE corp_id=? AND dingtalk_id=?", a.Config.DingCorp, dept) {
				fail(400, "钉钉部门不存在，请先同步通讯录")
			}
			corp = a.Config.DingCorp
			if exists(ctx, tx, "SELECT id FROM companies WHERE id<>? AND dingtalk_corp_id=? AND dingtalk_department_id=?", id, corp, dept) {
				fail(409, "该钉钉部门已绑定其他公司")
			}
		}
		if id == 0 {
			id = insert(ctx, tx, "INSERT INTO companies (code,name,parent_id,remark,dingtalk_corp_id,dingtalk_department_id) VALUES (?,?,?,?,?,?)", code, name, parent, remark, corp, dept)
		} else {
			exec(ctx, tx, "UPDATE companies SET code=?,name=?,parent_id=?,remark=?,dingtalk_corp_id=?,dingtalk_department_id=?,version=version+1,updated_at=CURRENT_TIMESTAMP(3) WHERE id=?", code, name, parent, remark, corp, dept, id)
		}
		exec(ctx, tx, "INSERT INTO company_names (name,company_id) VALUES (?,?) ON DUPLICATE KEY UPDATE company_id=company_id", name, id)
		// Current employee labels follow the mapping immediately; historical assets and approval snapshots remain unchanged.
		a.reassignEmployeeCompanies(ctx, tx)
		c := one(ctx, tx, "SELECT * FROM companies WHERE id=?", id)
		audit(ctx, tx, actor(r), "company_save", fmt.Sprint(id), Row{"before": old, "after": c})
		return c
	})
	status := 200
	if r.Method == "POST" {
		status = 201
	}
	send(w, status, Row{"company": result})
}

// Nearest explicitly bound ancestor wins. Employees in several departments retain every membership.
func resolveDepartmentCompany(id int64, departments map[int64]Row, bindings map[int64]int64, fallback int64) int64 {
	seen := map[int64]bool{}
	for id > 0 && !seen[id] {
		if company := bindings[id]; company > 0 {
			return company
		}
		seen[id] = true
		d := departments[id]
		if d == nil {
			break
		}
		id = num(d["parent_id"])
	}
	return fallback
}

func (a *App) reassignEmployeeCompanies(ctx context.Context, tx *sql.Tx) {
	companies := rows(ctx, tx, "SELECT id,name,dingtalk_department_id FROM companies WHERE dingtalk_corp_id=?", a.Config.DingCorp)
	bindings := map[int64]int64{}
	for _, c := range companies {
		bindings[num(c["dingtalk_department_id"])] = num(c["id"])
	}
	departments := map[int64]Row{}
	for _, d := range rows(ctx, tx, "SELECT * FROM departments WHERE corp_id=?", a.Config.DingCorp) {
		departments[num(d["dingtalk_id"])] = d
	}
	fallback := num(one(ctx, tx, "SELECT value FROM system_settings WHERE `key`='directory.defaultCompanyId'")["value"])
	for id, d := range departments {
		company := resolveDepartmentCompany(id, departments, bindings, fallback)
		var nullable any
		if company > 0 {
			nullable = company
		}
		exec(ctx, tx, "UPDATE employee_departments SET company_id=? WHERE department_id=?", nullable, d["id"])
	}
	// GROUP_CONCAT is intentionally avoided so long department/company lists are never silently truncated.
	memberships := rows(ctx, tx, "SELECT DISTINCT ed.employee_id,c.id,c.name,c.code FROM employee_departments ed JOIN companies c ON c.id=ed.company_id ORDER BY ed.employee_id,c.code,c.id")
	names := map[int64][]string{}
	for _, m := range memberships {
		id := num(m["employee_id"])
		names[id] = append(names[id], str(m["name"]))
	}
	for _, e := range rows(ctx, tx, "SELECT DISTINCT e.id FROM employees e JOIN employee_departments ed ON ed.employee_id=e.id") {
		// Display all company memberships in API relations. This legacy field is a bounded summary.
		summary := strings.Join(names[num(e["id"])], " / ")
		if len([]rune(summary)) > 200 {
			summary = string([]rune(summary)[:197]) + "…"
		}
		exec(ctx, tx, "UPDATE employees SET company=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=?", summary, e["id"])
	}
}

// Backfill only existing names, once per name. Never infer corporate ownership from similar names.
func SeedCompanies(ctx context.Context, db *sql.DB) {
	transaction(ctx, db, func(tx *sql.Tx) bool {
		exec(ctx, tx, "INSERT INTO company_names (name,company_id) SELECT name,id FROM companies ON DUPLICATE KEY UPDATE company_id=company_id")
		if exists(ctx, tx, "SELECT `key` FROM system_settings WHERE `key`='companies.seeded'") {
			return true
		}
		names := rows(ctx, tx, "SELECT DISTINCT company FROM (SELECT company FROM assets UNION SELECT company FROM employees) n WHERE company IS NOT NULL AND TRIM(company)<>'' ORDER BY company")
		for _, v := range names {
			name := strings.TrimSpace(str(v["company"]))
			if exists(ctx, tx, "SELECT id FROM companies WHERE name=?", name) {
				continue
			}
			next := num(one(ctx, tx, "SELECT COALESCE(MAX(id),0)+1 AS n FROM companies")["n"])
			code := fmt.Sprintf("C%04d", next)
			for exists(ctx, tx, "SELECT id FROM companies WHERE code=?", code) {
				next++
				code = fmt.Sprintf("C%04d", next)
			}
			exec(ctx, tx, "INSERT INTO companies (code,name) VALUES (?,?)", code, name)
			exec(ctx, tx, "INSERT INTO company_names (name,company_id) SELECT name,id FROM companies WHERE name=?", name)
		}
		if len(names) > 0 {
			exec(ctx, tx, "INSERT INTO system_settings (`key`,value) VALUES ('companies.seeded','1') ON DUPLICATE KEY UPDATE value='1'")
		}
		return true
	})
}
