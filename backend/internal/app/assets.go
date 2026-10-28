package app

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,31}-[A-Z0-9]{1,32}$`)
var amountFields = []string{"amount_taxed", "amount_untaxed", "tax_amount"}
var editFields = []string{"category_level1", "category_path", "name", "location", "company", "model", "cpu", "discrete_gpu", "memory", "gpu_model", "serial_number", "remark", "photo_url", "assignee", "department", "received_at", "amount_taxed", "amount_untaxed", "tax_amount"}
var ownershipFields = []string{"status", "assignee", "department", "location", "received_at"}
var statuses = []string{"空闲", "在用", "借出", "维修", "报废", "丢失"}

func fieldProblem(field string, v any) string {
	if contains(amountFields, field) {
		n, e := strconv.ParseFloat(str(v), 64)
		switch v.(type) {
		case string, nil:
			return "金额必须为非负数字且不超过一万亿"
		}
		if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1e12 {
			return "金额必须为非负数字且不超过一万亿"
		}
		return ""
	}
	s, ok := v.(string)
	limit := 200
	if field == "remark" || field == "photo_url" {
		limit = 2000
	}
	if !ok || len([]rune(s)) > limit {
		return fmt.Sprintf("%s 必须为文本且不超过 %d 字", field, limit)
	}
	if field == "name" && strings.TrimSpace(s) == "" {
		return "资产名称不能为空"
	}
	if field == "status" && !contains(statuses, s) {
		return "资产状态无效"
	}
	if field == "asset_code" && !codePattern.MatchString(s) {
		return "资产编码格式无效"
	}
	if (field == "created_at" || field == "received_at") && s != "" {
		if len(s) < 10 {
			return "日期请使用 YYYY-MM-DD"
		}
		if _, e := time.Parse("2006-01-02", s[:10]); e != nil {
			return "日期不存在，请核对年月日"
		}
	}
	return ""
}
func parseQR(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if codePattern.MatchString(raw) {
		return raw, "asset_code"
	}
	u, e := url.Parse(raw)
	if e == nil && u.Scheme != "" && u.Host != "" {
		parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
		last, e := url.PathUnescape(parts[len(parts)-1])
		if e == nil && codePattern.MatchString(last) {
			if u.Hostname() == "d.gongbeiyun.com" && parts[0] == "a" {
				return last, "gongbeiyun_url"
			}
			return last, "native_url"
		}
	}
	return "", "unknown"
}
func scope(u Row, available bool, alias string) (string, []any) {
	if !restricted(u) {
		return "1=1", nil
	}
	if alias == "" {
		alias = "assets"
	}
	p := alias + "."
	if available {
		return p + "status='空闲' AND " + p + "assignee_employee_id IS NULL AND COALESCE(" + p + "assignee,'')=''", nil
	}
	emp := u["employee_id"]
	if emp == nil {
		emp = -1
	}
	return "(" + p + "assignee_employee_id=? OR EXISTS (SELECT 1 FROM asset_flow_items si JOIN asset_flow_steps ss ON ss.flow_id=si.flow_id JOIN asset_flows sf ON sf.id=si.flow_id WHERE si.asset_id=" + p + "id AND ss.approver_id=? AND sf.status='pending'))", []any{emp, u["id"]}
}
func assetWhere(u Row, f url.Values) (string, []any) {
	clause, args := scope(u, f.Get("scope") == "available", "")
	if f.Get("scope") == "mine" {
		clause = "assignee_employee_id=?"
		emp := u["employee_id"]
		if emp == nil {
			emp = -1
		}
		args = []any{emp}
	}
	clauses := []string{clause}
	if s := f.Get("search"); s != "" {
		s = text(s, 200, false)
		fields := []string{"asset_code", "name", "model", "serial_number", "assignee", "department", "company"}
		if field := f.Get("searchField"); field != "" && field != "all" {
			if !contains(fields, field) {
				fail(400, "搜索字段无效")
			}
			fields = []string{field}
		}
		searches := []string{}
		for _, field := range fields {
			searches = append(searches, field+" COLLATE utf8mb4_0900_ai_ci LIKE ?")
		}
		clauses = append(clauses, "("+strings.Join(searches, " OR ")+")")
		for range fields {
			args = append(args, "%"+s+"%")
		}
	}
	for _, key := range []string{"status", "category", "location", "company", "department"} {
		if s := f.Get(key); s != "" {
			s = text(s, 200, false)
			field := key
			if key == "category" {
				field = "category_path"
			}
			clauses = append(clauses, field+"=?")
			args = append(args, s)
		}
	}
	if codes, ok := f["code"]; ok {
		if len(codes) == 0 || len(codes) > 100 {
			fail(400, "每次最多选择 100 项资产")
		}
		marks := []string{}
		for _, code := range codes {
			code = text(code, 100, true)
			if !codePattern.MatchString(code) {
				fail(400, "资产编码无效")
			}
			marks = append(marks, "?")
			args = append(args, code)
		}
		clauses = append(clauses, "asset_code IN ("+strings.Join(marks, ",")+")")
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func assetOrder(f url.Values) string {
	field := f.Get("sort")
	if field == "" {
		field = "updated_at_system"
	}
	if !contains([]string{"asset_code", "name", "category_path", "status", "location", "company", "model", "assignee", "department", "created_at", "received_at", "updated_at_system"}, field) {
		fail(400, "排序字段无效")
	}
	direction := strings.ToUpper(f.Get("direction"))
	if direction == "" {
		direction = "DESC"
	}
	if direction != "ASC" && direction != "DESC" {
		fail(400, "排序方向无效")
	}
	return " ORDER BY `" + field + "` " + direction + ",id " + direction
}

func (a *App) assetOptions(w http.ResponseWriter, r *http.Request) {
	f := url.Values{}
	f.Set("scope", r.URL.Query().Get("scope"))
	where, args := assetWhere(actor(r), f)
	ctx := r.Context()
	result := Row{"total": one(ctx, a.DB, "SELECT COUNT(*) n FROM assets"+where, args...)["n"]}
	for key, field := range map[string]string{"statuses": "status", "locations": "location", "companies": "company", "departments": "department", "categories": "category_path"} {
		result[key] = rows(ctx, a.DB, "SELECT COALESCE("+field+",'') name,COUNT(*) count FROM assets"+where+" GROUP BY "+field+" ORDER BY "+field, args...)
	}
	send(w, 200, result)
}
func assetDetail(ctx context.Context, q DBTX, code string) Row {
	a := one(ctx, q, "SELECT * FROM assets WHERE asset_code=?", code)
	if a == nil {
		fail(404, "资产不存在")
	}
	events := rows(ctx, q, "SELECT e.*,f.document_no FROM asset_events e LEFT JOIN asset_flows f ON f.id=e.flow_id WHERE e.asset_code=? ORDER BY e.id DESC", code)
	for _, event := range events {
		changes := []Row{}
		if str(event["changes_json"]) != "" {
			decode(str(event["changes_json"]), &changes)
		}
		event["changes"] = changes
	}
	a["events"] = events
	return a
}
func (a *App) listAssets(w http.ResponseWriter, r *http.Request) {
	p := page(r, "page", 1, 1000000)
	size := page(r, "pageSize", 20, 100)
	where, args := assetWhere(actor(r), r.URL.Query())
	total := one(r.Context(), a.DB, "SELECT COUNT(*) AS n FROM assets"+where, args...)["n"]
	queryArgs := append(append([]any{}, args...), size, (p-1)*size)
	send(w, 200, Row{"items": rows(r.Context(), a.DB, "SELECT * FROM assets"+where+assetOrder(r.URL.Query())+" LIMIT ? OFFSET ?", queryArgs...), "total": total, "page": p, "pageSize": size})
}
func (a *App) getAsset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := actor(r)
	code := r.PathValue("code")
	if restricted(u) {
		clause, args := scope(u, false, "")
		available, _ := scope(u, true, "")
		args = append([]any{code}, args...)
		if !exists(ctx, a.DB, "SELECT id FROM assets WHERE asset_code=? AND ("+clause+" OR ("+available+"))", args...) {
			fail(404, "资产不存在或无权查看")
		}
	}
	v := assetDetail(ctx, a.DB, code)
	if restricted(u) && v["status"] == "空闲" && (u["employee_id"] == nil || num(v["assignee_employee_id"]) != num(u["employee_id"])) {
		v["events"] = []Row{}
	}
	send(w, 200, v)
}
func (a *App) saveQR(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	raw := text(body(r)["raw"], 4000, true)
	code, format := parseQR(raw)
	if code != r.PathValue("code") {
		fail(400, "二维码内容与资产编码不匹配")
	}
	ctx := r.Context()
	result := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		v := one(ctx, tx, "SELECT * FROM assets WHERE asset_code=? FOR UPDATE", code)
		if v == nil {
			fail(404, "资产不存在")
		}
		if str(v["qr_raw"]) != "" {
			if v["qr_raw"] != raw {
				exec(ctx, tx, "INSERT INTO asset_qr_aliases (asset_id,raw) VALUES (?,?) ON DUPLICATE KEY UPDATE id=id", v["id"], raw)
			}
			return Row{"status": "saved", "qr_raw": v["qr_raw"], "qr_format": v["qr_format"]}
		}
		exec(ctx, tx, "UPDATE assets SET qr_raw=?,qr_format=?,version=version+1 WHERE id=?", raw, format, v["id"])
		return Row{"status": "saved", "qr_raw": raw, "qr_format": format}
	})
	send(w, 200, result)
}
func (a *App) updateAsset(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	b := body(r)
	ctx := r.Context()
	code := r.PathValue("code")
	result := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		v := one(ctx, tx, "SELECT * FROM assets WHERE asset_code=? FOR UPDATE", code)
		if v == nil {
			fail(404, "资产不存在")
		}
		expected := integer(b["expectedVersion"], 0, 9007199254740991)
		if expected != num(v["version"]) {
			fail(409, "资料已被其他人更新，请重新打开资产核对后保存")
		}
		for _, f := range ownershipFields {
			if value, ok := b[f]; ok && str(value) != str(v[f]) {
				fail(409, "使用人、部门、位置和领用日期需通过流转审批变更")
			}
		}
		updates := []string{}
		args := []any{}
		changes := []Row{}
		for _, f := range editFields {
			value, ok := b[f]
			if !ok {
				continue
			}
			if issue := fieldProblem(f, value); issue != "" {
				fail(400, issue)
			}
			if str(value) != str(v[f]) {
				updates = append(updates, "`"+f+"`=?")
				args = append(args, value)
				changes = append(changes, Row{"field": f, "before": v[f], "after": value})
			}
		}
		if len(updates) == 0 {
			fail(400, "没有需要保存的资产资料")
		}
		updates = append(updates, "version=version+1", "updated_at_system=CURRENT_TIMESTAMP(3)")
		args = append(args, v["id"])
		exec(ctx, tx, "UPDATE assets SET "+strings.Join(updates, ",")+" WHERE id=?", args...)
		id := writeEvent(ctx, tx, v, "编辑", str(actor(r)["display_name"]), text(b["note"], 2000, false), nil, nil, changes)
		return Row{"status": "updated", "asset": assetDetail(ctx, tx, code), "event": one(ctx, tx, "SELECT * FROM asset_events WHERE id=?", id)}
	})
	send(w, 200, result)
}
func writeEvent(ctx context.Context, q DBTX, old Row, kind, operator, note string, request, flow any, changes []Row) int64 {
	next := one(ctx, q, "SELECT * FROM assets WHERE id=?", old["id"])
	if changes == nil {
		changes = []Row{}
	}
	return insert(ctx, q, "INSERT INTO asset_events (asset_id,asset_code,type,operator_name,from_status,to_status,from_assignee,to_assignee,from_department,to_department,from_location,to_location,note,request_id,flow_id,changes_json) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", old["id"], old["asset_code"], kind, operator, str(old["status"]), str(next["status"]), str(old["assignee"]), str(next["assignee"]), str(old["department"]), str(next["department"]), str(old["location"]), str(next["location"]), note, request, flow, js(changes))
}
func (a *App) stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	where, args := scope(actor(r), false, "")
	where = " WHERE " + where
	total := one(ctx, a.DB, "SELECT COUNT(*) AS n FROM assets"+where, args...)["n"]
	status := Row{}
	for _, v := range rows(ctx, a.DB, "SELECT status,COUNT(*) AS n FROM assets"+where+" GROUP BY status", args...) {
		status[str(v["status"])] = v["n"]
	}
	v := Row{"total": total, "status": status}
	for k, f := range map[string]string{"categories": "category_path", "locations": "location", "companies": "company"} {
		v[k] = rows(ctx, a.DB, "SELECT COALESCE("+f+",'未设置') AS name,COUNT(*) AS count FROM assets"+where+" GROUP BY "+f+" ORDER BY count DESC", args...)
	}
	v["amounts"] = one(ctx, a.DB, "SELECT COALESCE(SUM(amount_taxed),0) AS taxed,COALESCE(SUM(amount_untaxed),0) AS untaxed,COALESCE(SUM(tax_amount),0) AS tax FROM assets"+where, args...)
	send(w, 200, v)
}
