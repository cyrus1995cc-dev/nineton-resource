package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (a *App) dingConfigured() bool {
	return a.Config.DingID != "" && a.Config.DingSecret != "" && a.Config.DingCorp != ""
}
func (a *App) settings(ctx context.Context) Row {
	values := map[string]string{}
	for _, v := range rows(ctx, a.DB, "SELECT `key`,value FROM system_settings WHERE `key` IN ('directory.roots','directory.interval','directory.defaultCompanyId')") {
		values[str(v["key"])] = str(v["value"])
	}
	roots := []int64{1}
	if values["directory.roots"] != "" {
		decode(values["directory.roots"], &roots)
	}
	interval, _ := strconv.ParseInt(values["directory.interval"], 10, 64)
	return Row{"rootDepartmentIds": roots, "intervalMinutes": interval, "defaultCompanyId": num(values["directory.defaultCompanyId"]), "configured": a.dingConfigured(), "corpId": a.Config.DingCorp}
}
func employeeList(ctx context.Context, q DBTX, search string, all bool) []Row {
	return rows(ctx, q, "SELECT e.id,e.name,e.company,e.department,e.corp_id,e.dingtalk_user_id,e.job_number,e.title,e.active,e.missing_since,e.synced_at,u.id AS user_id,u.role FROM employees e LEFT JOIN users u ON u.employee_id=e.id WHERE (? OR (e.active=1 AND e.missing_since IS NULL)) AND (e.name COLLATE utf8mb4_0900_ai_ci LIKE ? OR e.department COLLATE utf8mb4_0900_ai_ci LIKE ? OR e.job_number COLLATE utf8mb4_0900_ai_ci LIKE ?) ORDER BY e.name,e.id", all, "%"+search+"%", "%"+search+"%", "%"+search+"%")
}
func (a *App) employees(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list := employeeList(ctx, a.DB, text(r.URL.Query().Get("search"), 200, false), actor(r)["role"] == "admin" && r.URL.Query().Get("all") == "true")
	byID := map[int64]Row{}
	for _, e := range list {
		e["memberships"] = []Row{}
		byID[num(e["id"])] = e
	}
	for _, m := range rows(ctx, a.DB, "SELECT ed.employee_id,ed.department_id,ed.company_id,d.dingtalk_id,d.corp_id,d.name AS department_name,c.name AS company_name FROM employee_departments ed JOIN departments d ON d.id=ed.department_id LEFT JOIN companies c ON c.id=ed.company_id ORDER BY ed.employee_id,d.dingtalk_id") {
		if e := byID[num(m["employee_id"])]; e != nil {
			e["memberships"] = append(e["memberships"].([]Row), m)
		}
	}
	send(w, 200, Row{"employees": list})
}
func (a *App) directoryStatus(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	ctx := r.Context()
	v := a.settings(ctx)
	v["runs"] = rows(ctx, a.DB, "SELECT * FROM directory_sync_runs ORDER BY id DESC LIMIT 10")
	v["employeesCount"] = one(ctx, a.DB, "SELECT COUNT(*) AS n FROM employees")["n"]
	v["departments"] = rows(ctx, a.DB, "SELECT * FROM departments ORDER BY parent_id,dingtalk_id")
	// Every department uses the same snapshot timestamp. Stale departments stay visible for reconciliation.
	latestStamp := one(ctx, a.DB, "SELECT synced_at FROM departments WHERE corp_id=? ORDER BY synced_at DESC LIMIT 1", a.Config.DingCorp)
	for _, d := range v["departments"].([]Row) {
		d["missing"] = str(d["corp_id"]) != a.Config.DingCorp || str(d["synced_at"]) != str(latestStamp["synced_at"])
	}
	a.syncMu.Lock()
	v["syncing"] = a.syncing
	a.syncMu.Unlock()
	v["configuration"] = Row{"clientId": a.Config.DingID != "", "clientSecret": a.Config.DingSecret != "", "corpId": a.Config.DingCorp != ""}
	v["unmappedCount"] = one(ctx, a.DB, "SELECT COUNT(*) AS n FROM assets WHERE assignee_employee_id IS NULL AND COALESCE(assignee,'')<>''")["n"]
	send(w, 200, v)
}
func (a *App) directorySettings(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	unlock := a.directoryEdit()
	defer unlock()
	b := body(r)
	raw, ok := b["rootDepartmentIds"].([]any)
	if !ok || len(raw) < 1 || len(raw) > 100 {
		fail(400, "请设置 1 至 100 个授权起始部门编号")
	}
	roots := []int64{}
	seen := map[int64]bool{}
	for _, v := range raw {
		id := integer(v, 0, 9007199254740991)
		if !seen[id] {
			roots = append(roots, id)
			seen[id] = true
		}
	}
	interval, e := strconv.ParseInt(str(b["intervalMinutes"]), 10, 64)
	if e != nil || interval != 0 && (interval < 30 || interval > 1440) {
		fail(400, "同步间隔为 0（关闭）或 30 至 1440 分钟")
	}
	ctx := r.Context()
	transaction(ctx, a.DB, func(tx *sql.Tx) bool {
		if v, ok := b["defaultCompanyId"]; ok {
			id := int64(0)
			if v != nil && str(v) != "0" {
				id = integer(v, 0, 9007199254740991)
			}
			if id > 0 && !exists(ctx, tx, "SELECT id FROM companies WHERE id=?", id) {
				fail(400, "默认所属公司不存在")
			}
			exec(ctx, tx, "INSERT INTO system_settings (`key`,value) VALUES ('directory.defaultCompanyId',?) ON DUPLICATE KEY UPDATE value=?", str(id), str(id))
		}
		for key, value := range map[string]string{"directory.roots": js(roots), "directory.interval": str(interval)} {
			exec(ctx, tx, "INSERT INTO system_settings (`key`,value) VALUES (?,?) ON DUPLICATE KEY UPDATE value=?", key, value, value)
		}
		a.reassignEmployeeCompanies(ctx, tx)
		audit(ctx, tx, actor(r), "directory_settings", "dingtalk", b)
		return true
	})
	send(w, 200, a.settings(ctx))
}
func (a *App) employeeStatus(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	id := routeID(r, "id")
	b := body(r)
	reason := text(b["reason"], 500, true)
	active, ok := b["active"].(bool)
	if !ok {
		fail(400, "员工状态无效")
	}
	if id == num(actor(r)["employee_id"]) {
		fail(409, "不能在当前会话停用本人身份")
	}
	ctx := r.Context()
	transaction(ctx, a.DB, func(tx *sql.Tx) bool {
		if !exists(ctx, tx, "SELECT id FROM employees WHERE id=? FOR UPDATE", id) {
			fail(404, "员工不存在")
		}
		if !active && exists(ctx, tx, "SELECT s.id FROM asset_flow_steps s JOIN users u ON u.id=s.approver_id WHERE u.employee_id=? AND s.status IN ('pending','waiting')", id) {
			fail(409, "员工仍有待处理审批，请先处理单据")
		}
		exec(ctx, tx, "UPDATE employees SET active=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=?", active, id)
		exec(ctx, tx, "DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE employee_id=?)", id)
		audit(ctx, tx, actor(r), "employee_status", str(id), Row{"active": active, "reason": reason})
		return true
	})
	send(w, 200, Row{"ok": true})
}
func (a *App) mappings(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	ctx := r.Context()
	assets := rows(ctx, a.DB, "SELECT id,asset_code,name,assignee,department,version FROM assets WHERE assignee_employee_id IS NULL AND COALESCE(assignee,'')<>'' ORDER BY assignee,asset_code")
	for _, v := range assets {
		v["candidates"] = rows(ctx, a.DB, "SELECT id,name,department,job_number FROM employees WHERE name=? AND active=1 AND missing_since IS NULL", v["assignee"])
	}
	send(w, 200, Row{"assets": assets})
}
func (a *App) bindEmployee(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	b := body(r)
	id := integer(b["employeeId"], 0, 9007199254740991)
	version := integer(b["expectedVersion"], 0, 9007199254740991)
	reason := text(b["reason"], 500, true)
	ctx := r.Context()
	transaction(ctx, a.DB, func(tx *sql.Tx) bool {
		asset := one(ctx, tx, "SELECT * FROM assets WHERE asset_code=? FOR UPDATE", r.PathValue("code"))
		employee := one(ctx, tx, "SELECT * FROM employees WHERE id=? AND active=1 AND missing_since IS NULL FOR SHARE", id)
		if asset == nil || employee == nil {
			fail(404, "资产或有效员工不存在")
		}
		if num(asset["version"]) != version {
			fail(409, "台账已变化，请刷新后重新核对")
		}
		if num(asset["assignee_employee_id"]) == id {
			return true
		}
		if asset["assignee_employee_id"] != nil {
			fail(409, "资产已关联员工，更换使用人请通过交接审批")
		}
		if asset["assignee"] != employee["name"] || str(asset["assignee"]) == "" {
			fail(409, "旧台账姓名与员工姓名不一致，请通过交接审批变更")
		}
		if exists(ctx, tx, "SELECT i.id FROM asset_flow_items i JOIN asset_flows f ON f.id=i.flow_id WHERE i.asset_id=? AND f.status='pending'", asset["id"]) {
			fail(409, "资产有待审批单据，请先处理后关联")
		}
		exec(ctx, tx, "UPDATE assets SET assignee_employee_id=?,version=version+1,updated_at_system=CURRENT_TIMESTAMP(3) WHERE id=?", id, asset["id"])
		writeEvent(ctx, tx, asset, "人员关联", str(actor(r)["display_name"]), reason, nil, nil, []Row{{"field": "assignee_employee_id", "before": "", "after": id}})
		audit(ctx, tx, actor(r), "bind_employee", str(asset["asset_code"]), Row{"employeeId": id, "reason": reason})
		return true
	})
	send(w, 200, Row{"ok": true})
}
func (a *App) dingJSON(ctx context.Context, endpoint string, payload any, token string) Row {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewBufferString(js(payload))
	}
	method := "POST"
	if payload == nil {
		method = "GET"
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, reader)
	must(e)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("x-acs-dingtalk-access-token", token)
	}
	resp, e := a.client.Do(req)
	if e != nil {
		fail(502, "连接钉钉超时或网络不可用")
	}
	defer resp.Body.Close()
	var data Row
	d := json.NewDecoder(io.LimitReader(resp.Body, 16<<20))
	d.UseNumber()
	if e = d.Decode(&data); e != nil || resp.StatusCode >= 300 || data == nil {
		fail(502, "钉钉请求失败，请核对应用配置")
	}
	if v, ok := data["errcode"]; ok && num(v) != 0 {
		code := num(v)
		if code == 40014 || code == 42001 {
			panic(dingTokenExpired{})
		}
		fail(502, fmt.Sprintf("钉钉接口返回错误 %d，请核对应用凭证、通讯录读取权限和授权范围", code))
	}
	return data
}
func (a *App) dingBase() string {
	if a.Config.DingAPI != "" {
		return strings.TrimRight(a.Config.DingAPI, "/")
	}
	return "https://api.dingtalk.com"
}
func (a *App) dingToken(ctx context.Context) string {
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()
	if a.token != "" && time.Now().Before(a.tokenUntil) {
		return a.token
	}
	data := a.dingJSON(ctx, a.dingBase()+"/v1.0/oauth2/accessToken", Row{"appKey": a.Config.DingID, "appSecret": a.Config.DingSecret}, "")
	token := str(data["accessToken"])
	if token == "" {
		fail(502, "钉钉未返回有效令牌")
	}
	a.token = token
	expiry := num(data["expireIn"])
	if expiry == 0 {
		expiry = 7200
	}
	a.tokenUntil = time.Now().Add(time.Duration(expiry-120) * time.Second)
	return token
}

type dingTokenExpired struct{}

func (a *App) dingCall(ctx context.Context, path string, payload any) Row {
	base := a.Config.DingOAPI
	if base == "" {
		base = "https://oapi.dingtalk.com"
	}
	for attempt := 0; attempt < 2; attempt++ {
		var expired bool
		var data Row
		func() {
			defer func() {
				if p := recover(); p != nil {
					if _, ok := p.(dingTokenExpired); ok {
						expired = true
					} else {
						panic(p)
					}
				}
			}()
			token := a.dingToken(ctx)
			data = a.dingJSON(ctx, strings.TrimRight(base, "/")+path+"?access_token="+url.QueryEscape(token), payload, "")
		}()
		if !expired {
			return data
		}
		a.tokenMu.Lock()
		a.token = ""
		a.tokenUntil = time.Time{}
		a.tokenMu.Unlock()
	}
	fail(502, "钉钉令牌刷新失败，请核对应用凭证")
	return nil
}
func (a *App) dingLogin(w http.ResponseWriter, r *http.Request) {
	if !a.dingConfigured() {
		fail(503, "钉钉登录尚未配置")
	}
	code := text(body(r)["code"], 2000, true)
	// H5 requestAuthCode returns an enterprise login code, not a user OAuth code.
	data := a.dingCall(r.Context(), "/topapi/v2/user/getuserinfo", Row{"code": code})
	identity, _ := data["result"].(map[string]any)
	userID := str(identity["userid"])
	if userID == "" {
		fail(502, "钉钉未返回有效企业用户标识")
	}
	ctx := r.Context()
	result := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		es := rows(ctx, tx, "SELECT * FROM employees WHERE corp_id=? AND dingtalk_user_id=? FOR UPDATE", a.Config.DingCorp, userID)
		if len(es) != 1 {
			fail(403, "当前钉钉身份尚未关联企业通讯录")
		}
		e := es[0]
		if num(e["active"]) != 1 || e["missing_since"] != nil {
			fail(403, "钉钉账号已停用或授权范围待核对")
		}
		ensureEmployeeUser(ctx, tx, e["id"])
		u := one(ctx, tx, userJoin+"WHERE u.employee_id=? AND "+validUser, e["id"])
		if u == nil {
			fail(403, "账号已停用")
		}
		v := a.session(ctx, tx, u)
		v["status"] = "authenticated"
		return v
	})
	send(w, 200, result)
}
func (a *App) directorySync(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	send(w, 200, a.syncDirectory(r.Context(), actor(r)))
}
func (a *App) claimDirectorySync(ctx context.Context, u Row) int64 {
	if !a.dingConfigured() {
		fail(503, "钉钉应用尚未配置")
	}
	a.syncMu.Lock()
	if a.syncing {
		a.syncMu.Unlock()
		fail(409, "通讯录正在同步")
	}
	a.syncing = true
	a.syncMu.Unlock()
	defer func() {
		if p := recover(); p != nil {
			a.releaseDirectorySync()
			panic(p)
		}
	}()
	var actorID any
	if u != nil {
		actorID = u["id"]
	}
	return insert(ctx, a.DB, "INSERT INTO directory_sync_runs (actor_id,status) VALUES (?,'running')", actorID)
}
func (a *App) releaseDirectorySync() { a.syncMu.Lock(); a.syncing = false; a.syncMu.Unlock() }

// Called once on process startup, before accepting new sync jobs.
func (a *App) RecoverDirectorySync(ctx context.Context) {
	exec(ctx, a.DB, "UPDATE directory_sync_runs SET status='failed',message='服务重启中断了同步，请重新同步；原有通讯录已保留',completed_at=CURRENT_TIMESTAMP(3) WHERE status='running'")
}
func (a *App) syncDirectory(ctx context.Context, u Row) Row {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	run := a.claimDirectorySync(ctx, u)
	return a.performDirectorySync(ctx, u, run)
}
func (a *App) startDirectorySync(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	run := a.claimDirectorySync(r.Context(), actor(r))
	u := actor(r)
	go func() {
		defer func() {
			if recover() != nil {
				slog.Warn("background directory sync failed", "run", run)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		a.performDirectorySync(ctx, u, run)
	}()
	send(w, 202, Row{"runId": run, "status": "running"})
}
func (a *App) performDirectorySync(ctx context.Context, u Row, run int64) (out Row) {
	defer a.releaseDirectorySync()
	defer func() {
		if p := recover(); p != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			message := "同步失败，原有人员数据已保留"
			if e, ok := p.(problem); ok {
				message = e.Message
			}
			_, _ = a.DB.ExecContext(cleanup, "UPDATE directory_sync_runs SET status='failed',message=?,completed_at=CURRENT_TIMESTAMP(3) WHERE id=?", message, run)
			panic(p)
		}
	}()
	cfg := a.settings(ctx)
	queue := append([]int64{}, cfg["rootDepartmentIds"].([]int64)...)
	visited := map[int64]bool{}
	departments := map[int64]Row{}
	people := map[string]Row{}
	members := map[string]map[int64]bool{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		if len(visited) > 10000 {
			fail(502, "部门数量超过限制")
		}
		detail, _ := a.dingCall(ctx, "/topapi/v2/department/get", Row{"dept_id": id, "language": "zh_CN"})["result"].(map[string]any)
		if detail == nil || str(detail["name"]) == "" || len([]rune(str(detail["name"]))) > 200 {
			fail(502, "钉钉部门详情格式异常")
		}
		departments[id] = detail
		children, ok := a.dingCall(ctx, "/topapi/v2/department/listsub", Row{"dept_id": id, "language": "zh_CN"})["result"].([]any)
		if !ok {
			fail(502, "钉钉子部门数据格式异常")
		}
		for _, raw := range children {
			child, ok := raw.(map[string]any)
			if !ok {
				fail(502, "钉钉部门数据异常")
			}
			queue = append(queue, integer(child["dept_id"], 0, 9007199254740991))
		}
		cursor := int64(0)
		cursors := map[int64]bool{}
		for {
			if cursors[cursor] || len(cursors) > 2000 {
				fail(502, "钉钉分页游标异常")
			}
			cursors[cursor] = true
			page, ok := a.dingCall(ctx, "/topapi/v2/user/list", Row{"dept_id": id, "cursor": cursor, "size": 100, "contain_access_limit": false, "language": "zh_CN"})["result"].(map[string]any)
			if !ok {
				fail(502, "钉钉员工数据格式异常")
			}
			list, ok := page["list"].([]any)
			if !ok {
				fail(502, "钉钉员工数据格式异常")
			}
			for _, raw := range list {
				p, ok := raw.(map[string]any)
				if !ok || str(p["userid"]) == "" || str(p["name"]) == "" {
					fail(502, "钉钉员工缺少身份字段")
				}
				for _, field := range []string{"userid", "name", "unionid", "job_number", "title"} {
					if len([]rune(str(p[field]))) > 200 {
						fail(502, "钉钉员工字段超长，本次同步未保存")
					}
				}
				key := str(p["userid"])
				people[key] = p
				if len(people) > 100000 {
					fail(502, "通讯录人数超过限制")
				}
				if members[key] == nil {
					members[key] = map[int64]bool{}
				}
				members[key][id] = true
			}
			more, valid := page["has_more"].(bool)
			if !valid {
				fail(502, "钉钉员工分页标识异常，本次同步未保存")
			}
			if !more {
				break
			}
			cursor = integer(page["next_cursor"], 0, 9007199254740991)
		}
	}
	for id := range departments {
		seen := map[int64]bool{}
		for next := id; departments[next] != nil; next = num(departments[next]["parent_id"]) {
			if seen[next] {
				fail(502, "钉钉部门上下级关系存在循环，本次同步未保存")
			}
			seen[next] = true
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		for id, d := range departments {
			parent := d["parent_id"]
			exec(ctx, tx, "INSERT INTO departments (corp_id,dingtalk_id,parent_id,name,synced_at) VALUES (?,?,?,?,?) ON DUPLICATE KEY UPDATE parent_id=?,name=?,synced_at=?", a.Config.DingCorp, id, parent, d["name"], now, parent, d["name"], now)
		}
		for key, p := range people {
			ids := []int64{}
			for id := range members[key] {
				ids = append(ids, id)
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			names := []string{}
			for _, id := range ids {
				names = append(names, str(departments[id]["name"]))
			}
			department := strings.Join(names, " / ")
			if len([]rune(department)) > 200 {
				department = string([]rune(department)[:197]) + "…"
			}
			union := p["unionid"]
			if str(union) == "" {
				union = nil
			}
			if exists(ctx, tx, "SELECT id FROM employees WHERE dingtalk_user_id=? AND corp_id<>?", key, a.Config.DingCorp) {
				fail(409, "发现不同企业的重复员工标识，请核对钉钉企业配置，原通讯录已保留")
			}
			exec(ctx, tx, "INSERT INTO employees (name,department,dingtalk_user_id,corp_id,union_id,job_number,title,department_ids_json,synced_at) VALUES (?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE name=?,department=?,union_id=?,job_number=?,title=?,department_ids_json=?,synced_at=?,missing_since=NULL,updated_at=CURRENT_TIMESTAMP(3)", p["name"], department, key, a.Config.DingCorp, union, str(p["job_number"]), str(p["title"]), js(ids), now, p["name"], department, union, str(p["job_number"]), str(p["title"]), js(ids), now)
			employee := one(ctx, tx, "SELECT id FROM employees WHERE corp_id=? AND dingtalk_user_id=?", a.Config.DingCorp, key)
			exec(ctx, tx, "DELETE FROM employee_departments WHERE employee_id=?", employee["id"])
			for _, id := range ids {
				d := one(ctx, tx, "SELECT id FROM departments WHERE corp_id=? AND dingtalk_id=?", a.Config.DingCorp, id)
				exec(ctx, tx, "INSERT INTO employee_departments (employee_id,department_id) VALUES (?,?)", employee["id"], d["id"])
			}
			ensureEmployeeUser(ctx, tx, employee["id"])
		}
		a.reassignEmployeeCompanies(ctx, tx)
		exec(ctx, tx, "UPDATE employees SET missing_since=COALESCE(missing_since,?) WHERE corp_id=? AND (synced_at IS NULL OR synced_at<>?)", now, a.Config.DingCorp, now)
		exec(ctx, tx, "DELETE FROM sessions WHERE user_id IN (SELECT u.id FROM users u JOIN employees e ON e.id=u.employee_id WHERE e.corp_id=? AND e.missing_since IS NOT NULL)", a.Config.DingCorp)
		exec(ctx, tx, "UPDATE directory_sync_runs SET status='completed',employees_count=?,departments_count=?,completed_at=CURRENT_TIMESTAMP(3) WHERE id=?", len(people), len(departments), run)
		out := Row{"employees": len(people), "departments": len(departments)}
		audit(ctx, tx, u, "directory_sync", fmt.Sprint(run), out)
		return out
	})
}
