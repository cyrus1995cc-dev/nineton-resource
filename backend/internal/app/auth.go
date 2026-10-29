package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

var userRoles = []string{"admin", "asset_manager", "viewer", "employee", "approver", "guest"}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
func hash(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func PasswordHash(password string) string {
	b := make([]byte, 16)
	_, e := rand.Read(b)
	must(e)
	salt := hex.EncodeToString(b)
	key, e := scrypt.Key([]byte(password), []byte(salt), 16384, 8, 1, 64)
	must(e)
	return "scrypt$" + salt + "$" + hex.EncodeToString(key)
}
func VerifyPassword(password, stored string) bool {
	p := strings.Split(stored, "$")
	if len(p) != 3 || p[0] != "scrypt" || len(password) > 200 {
		return false
	}
	expected, e := hex.DecodeString(p[2])
	if e != nil || len(expected) != 64 {
		return false
	}
	actual, e := scrypt.Key([]byte(password), []byte(p[1]), 16384, 8, 1, 64)
	return e == nil && subtle.ConstantTimeCompare(actual, expected) == 1
}
func publicUser(u Row) Row {
	return Row{"id": u["id"], "username": u["username"], "display_name": u["display_name"], "role": u["role"], "employee_id": u["employee_id"], "department": str(u["employee_department"])}
}

const userJoin = "SELECT u.*, e.department AS employee_department FROM users u LEFT JOIN employees e ON e.id=u.employee_id "
const validUser = "u.active=1 AND (e.id IS NULL OR (e.active=1 AND e.missing_since IS NULL))"

func (a *App) Bootstrap(ctx context.Context) {
	a.ensureCatalogue(ctx)
	if exists(ctx, a.DB, "SELECT id FROM users LIMIT 1") {
		return
	}
	if len(a.Config.AdminPassword) < 12 || len(a.Config.AdminPassword) > 200 {
		fail(400, "首次启动请通过 ASSET_ADMIN_PASSWORD 设置 12 至 200 位管理员密码")
	}
	name := a.Config.AdminUsername
	if name == "" {
		name = "admin"
	}
	exec(ctx, a.DB, "INSERT INTO users (username,password_hash,display_name,role) VALUES (?,?,?,'admin')", name, PasswordHash(a.Config.AdminPassword), "系统管理员")
}
func (a *App) resolve(ctx context.Context, token string) Row {
	if token == "" {
		return nil
	}
	now := time.Now().UnixMilli()
	u := one(ctx, a.DB, userJoin+"JOIN sessions s ON s.user_id=u.id LEFT JOIN session_policies p ON p.token_hash=s.token_hash WHERE s.token_hash=? AND s.expires_at>? AND (p.idle_until IS NULL OR p.idle_until>?) AND "+validUser, hash(token), now, now)
	if u == nil {
		return nil
	}
	exec(ctx, a.DB, "UPDATE session_policies SET idle_until=LEAST(?,(SELECT expires_at FROM sessions WHERE token_hash=?)) WHERE token_hash=? AND idle_until>? AND idle_until<?", time.Now().Add(30*time.Minute).UnixMilli(), hash(token), hash(token), now, time.Now().Add(25*time.Minute).UnixMilli())
	return publicUser(u)
}
func (a *App) session(ctx context.Context, q DBTX, u Row) Row {
	token := randomID()
	expires := time.Now().Add(8 * time.Hour).UnixMilli()
	exec(ctx, q, "INSERT INTO sessions (user_id,token_hash,expires_at) VALUES (?,?,?)", u["id"], hash(token), expires)
	exec(ctx, q, "INSERT INTO session_policies (token_hash,idle_until) VALUES (?,?)", hash(token), time.Now().Add(30*time.Minute).UnixMilli())
	return Row{"token": token, "user": publicUser(u), "expiresAt": expires}
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	b := body(r)
	name := text(b["username"], 100, true)
	password, validPassword := b["password"].(string)
	if !validPassword || len(password) == 0 || len(password) > 200 {
		fail(400, "请填写有效密码")
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	key := ip + ":" + name
	a.loginMu.Lock()
	entry := a.attempts[key]
	if time.Now().After(entry.until) {
		entry = attempt{until: time.Now().Add(10 * time.Minute)}
	}
	if entry.count >= 10 {
		a.loginMu.Unlock()
		fail(429, "登录尝试过多，请十分钟后重试")
	}
	if len(a.attempts) > 5000 {
		for k, v := range a.attempts {
			if time.Now().After(v.until) {
				delete(a.attempts, k)
			}
		}
		if len(a.attempts) > 5000 {
			a.loginMu.Unlock()
			fail(429, "登录繁忙，请稍后重试")
		}
	}
	entry.count++
	a.attempts[key] = entry
	a.loginMu.Unlock()
	select {
	case a.authWork <- struct{}{}:
		defer func() { <-a.authWork }()
	default:
		fail(429, "登录繁忙，请稍后重试")
	}
	ctx := r.Context()
	u := one(ctx, a.DB, userJoin+"WHERE u.username=? AND "+validUser, name)
	if u == nil || !VerifyPassword(password, str(u["password_hash"])) {
		fail(401, "用户名或密码错误")
	}
	a.loginMu.Lock()
	delete(a.attempts, key)
	a.loginMu.Unlock()
	send(w, 200, a.session(ctx, a.DB, u))
}
func users(ctx context.Context, q DBTX) []Row {
	out := []Row{}
	identities := map[int64]Row{}
	for _, identity := range rows(ctx, q, "SELECT issuer,subject,user_id FROM external_identities") {
		identities[num(identity["user_id"])] = Row{"issuer": identity["issuer"], "subject": identity["subject"]}
	}
	for _, u := range rows(ctx, q, userJoin+"ORDER BY u.id") {
		v := publicUser(u)
		v["active"] = num(u["active"]) == 1
		v["created_at"] = u["created_at"]
		identity, bound := identities[num(u["id"])]
		v["sso_bound"] = bound
		if bound {
			v["sso_identity"] = identity
		}
		out = append(out, v)
	}
	return out
}
func (a *App) users(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	send(w, 200, Row{"users": users(r.Context(), a.DB)})
}
func (a *App) createUser(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	b := body(r)
	name := text(b["username"], 100, true)
	password, ok := b["password"].(string)
	if !ok || len(password) < 6 || len(password) > 200 {
		fail(400, "密码长度需为 6 至 200 位")
	}
	role := str(b["role"])
	if !contains(userRoles, role) {
		fail(400, "用户角色无效")
	}
	display := text(b["display_name"], 200, false)
	if display == "" {
		display = name
	}
	id := insert(r.Context(), a.DB, "INSERT INTO users (username,password_hash,display_name,role) VALUES (?,?,?,?)", name, PasswordHash(password), display, role)
	u := publicUser(one(r.Context(), a.DB, userJoin+"WHERE u.id=?", id))
	u["active"] = true
	u["created_at"] = one(r.Context(), a.DB, "SELECT created_at FROM users WHERE id=?", id)["created_at"]
	send(w, 201, Row{"status": "created", "user": u})
}
func (a *App) updateUser(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	id := routeID(r, "id")
	b := body(r)
	ctx := r.Context()
	result := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		u := one(ctx, tx, "SELECT * FROM users WHERE id=? FOR UPDATE", id)
		if u == nil {
			fail(404, "账号不存在")
		}
		role := str(u["role"])
		if v, ok := b["role"]; ok {
			role = str(v)
		}
		if !contains(userRoles, role) {
			fail(400, "用户角色无效")
		}
		active := num(u["active"]) == 1
		if v, ok := b["active"]; ok {
			var valid bool
			active, valid = v.(bool)
			if !valid {
				fail(400, "账号状态无效")
			}
		}
		if id == num(actor(r)["id"]) && (role != u["role"] || !active) {
			fail(409, "不能在当前会话修改自己的角色或停用自己")
		}
		if (!active || !contains([]string{"admin", "asset_manager", "approver"}, role)) && exists(ctx, tx, "SELECT id FROM asset_flow_steps WHERE approver_id=? AND status IN ('pending','waiting')", id) {
			fail(409, "该账号仍有待处理审批，请先处理单据后调整")
		}
		employee := u["employee_id"]
		if v, ok := b["employee_id"]; ok {
			employee = v
			if v != nil {
				employee = integer(v, 0, 9007199254740991)
			}
		}
		if employee != nil {
			if !exists(ctx, tx, "SELECT id FROM employees WHERE id=? AND active=1 AND missing_since IS NULL", employee) {
				fail(400, "关联员工不存在或已停用")
			}
			if exists(ctx, tx, "SELECT id FROM users WHERE employee_id=? AND id<>?", employee, id) {
				fail(409, "该员工已关联其他账号")
			}
		}
		exec(ctx, tx, "UPDATE users SET role=?, active=?, employee_id=?, updated_at=CURRENT_TIMESTAMP(3) WHERE id=?", role, active, employee, id)
		exec(ctx, tx, "DELETE FROM sessions WHERE user_id=?", id)
		audit(ctx, tx, actor(r), "user_permissions", str(id), Row{"before": publicUser(u), "after": Row{"role": role, "active": active, "employee_id": employee}})
		return publicUser(one(ctx, tx, userJoin+"WHERE u.id=?", id))
	})
	send(w, 200, Row{"user": result})
}
func audit(ctx context.Context, q DBTX, u Row, action, target string, details any) {
	var id any
	name := "定时同步"
	if u != nil {
		id = u["id"]
		name = str(u["display_name"])
	}
	exec(ctx, q, "INSERT INTO audit_logs (actor_id,actor_name,action,target,details_json) VALUES (?,?,?,?,?)", id, name, action, target, js(details))
}
func ensureEmployeeUser(ctx context.Context, q DBTX, id any) {
	e := one(ctx, q, "SELECT * FROM employees WHERE id=?", id)
	if e == nil {
		fail(404, "员工不存在")
	}
	u := one(ctx, q, "SELECT id FROM users WHERE employee_id=?", id)
	if u != nil {
		exec(ctx, q, "UPDATE users SET display_name=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=?", e["name"], u["id"])
		return
	}
	if str(e["union_id"]) != "" {
		legacy := one(ctx, q, "SELECT id,employee_id FROM users WHERE dingtalk_user_id=?", e["union_id"])
		if legacy != nil {
			if legacy["employee_id"] != nil && num(legacy["employee_id"]) != num(id) {
				fail(409, "钉钉身份已关联其他员工")
			}
			exec(ctx, q, "UPDATE users SET employee_id=? WHERE id=?", id, legacy["id"])
			return
		}
	}
	exec(ctx, q, "INSERT INTO users (username,password_hash,display_name,role,employee_id) VALUES (?,?,?,'employee',?)", "ding_"+hash(str(e["corp_id"]) + ":" + str(e["dingtalk_user_id"]))[:20], "external:dingtalk", e["name"], id)
}
