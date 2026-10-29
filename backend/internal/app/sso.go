package app

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Login contexts are short lived and browser bound. A restart safely requires
// a new login; no upstream token or client secret is stored in the database.
type ssoAttempt struct {
	browserHash, nonce, verifier, returnPath string
	expires                                  time.Time
}
type ssoState struct {
	mu       sync.Mutex
	provider *oidc.Provider
	oauth    *oauth2.Config
	attempts map[string]ssoAttempt
}

func (a *App) ssoConfigured() bool {
	return a.Config.SSOIssuer != "" && a.Config.SSOClientID != "" && a.Config.SSOClientSecret != "" && a.Config.SSORedirectURI != ""
}

func validSSOURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))
}

func (a *App) ssoClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, Transport: a.client.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (a *App) ssoProvider(ctx context.Context) (*oidc.Provider, *oauth2.Config) {
	if !a.ssoConfigured() {
		fail(503, "九吨统一登录尚未配置")
	}
	if !validSSOURL(a.Config.SSOIssuer) || !validSSOURL(a.Config.SSORedirectURI) || len(a.Config.SSOIssuer) > 200 {
		fail(503, "统一登录地址配置无效，请联系管理员")
	}
	a.sso.mu.Lock()
	defer a.sso.mu.Unlock()
	if a.sso.provider != nil {
		return a.sso.provider, a.sso.oauth
	}
	ctx = oidc.ClientContext(ctx, a.ssoClient())
	p, err := oidc.NewProvider(ctx, a.Config.SSOIssuer)
	if err != nil {
		fail(502, "暂时无法连接九吨统一登录，请稍后重试")
	}
	var metadata struct {
		JWKS        string   `json:"jwks_uri"`
		PKCE        []string `json:"code_challenge_methods_supported"`
		AuthMethods []string `json:"token_endpoint_auth_methods_supported"`
	}
	if p.Claims(&metadata) != nil || !contains(metadata.PKCE, "S256") {
		fail(502, "统一登录暂不支持所需的安全授权方式")
	}
	issuer, _ := url.Parse(a.Config.SSOIssuer)
	for _, raw := range []string{p.Endpoint().AuthURL, p.Endpoint().TokenURL, metadata.JWKS} {
		u, e := url.Parse(raw)
		if e != nil || !validSSOURL(raw) || u.Scheme != issuer.Scheme || u.Host != issuer.Host {
			fail(502, "统一登录服务地址校验失败")
		}
	}
	scopes := strings.Fields(a.Config.SSOScopes)
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	if !contains(scopes, oidc.ScopeOpenID) {
		fail(503, "统一登录配置缺少 openid 范围")
	}
	endpoint := p.Endpoint()
	endpoint.AuthStyle = oauth2.AuthStyleInHeader
	if len(metadata.AuthMethods) > 0 && !contains(metadata.AuthMethods, "client_secret_basic") {
		if !contains(metadata.AuthMethods, "client_secret_post") {
			fail(502, "统一登录客户端认证方式不兼容")
		}
		endpoint.AuthStyle = oauth2.AuthStyleInParams
	}
	a.sso.provider = p
	a.sso.oauth = &oauth2.Config{ClientID: a.Config.SSOClientID, ClientSecret: a.Config.SSOClientSecret, RedirectURL: a.Config.SSORedirectURI, Endpoint: endpoint, Scopes: scopes}
	return a.sso.provider, a.sso.oauth
}

func (a *App) secureCookies() bool { return strings.HasPrefix(a.Config.SSORedirectURI, "https://") }
func (a *App) sessionCookieName() string {
	if a.secureCookies() {
		return "__Host-asset_session"
	}
	return "asset_session"
}
func (a *App) contextCookieName() string {
	if a.secureCookies() {
		return "__Host-asset_oidc_context"
	}
	return "asset_oidc_context"
}

func (a *App) cookieToken(r *http.Request) string {
	if c, e := r.Cookie(a.sessionCookieName()); e == nil && len(c.Value) == 48 {
		return c.Value
	}
	return ""
}
func (a *App) requestToken(r *http.Request) string {
	if token := bearer(r); token != "" {
		return token
	}
	return a.cookieToken(r)
}
func (a *App) cookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}
func (a *App) checkRequestOrigin(r *http.Request) {
	origin := r.Header.Get("Origin")
	expected := ""
	if u, e := url.Parse(a.Config.SSORedirectURI); e == nil && u.Host != "" {
		expected = u.Scheme + "://" + u.Host
	}
	if expected == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		expected = scheme + "://" + r.Host
	}
	if origin != expected {
		fail(403, "请求来源校验失败，请刷新后重试")
	}
}
func (a *App) logoutSession(w http.ResponseWriter, r *http.Request) {
	for _, token := range []string{bearer(r), a.cookieToken(r)} {
		if token != "" {
			exec(r.Context(), a.DB, "DELETE FROM sessions WHERE token_hash=?", hash(token))
		}
	}
	if c, e := r.Cookie(a.contextCookieName()); e == nil {
		a.sso.mu.Lock()
		for state, attempt := range a.sso.attempts {
			if attempt.browserHash == hash(c.Value) {
				delete(a.sso.attempts, state)
			}
		}
		a.sso.mu.Unlock()
	}
	a.cookie(w, a.sessionCookieName(), "", -1)
	a.cookie(w, a.contextCookieName(), "", -1)
}

// Only the application's root/login pages and a numeric flow deep link are
// supported return destinations. Never reflect arbitrary redirect URLs.
func ssoReturnPath(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || u.IsAbs() || u.Host != "" || u.Opaque != "" || u.Fragment != "" || (u.Path != "/" && u.Path != "/login") {
		return "/"
	}
	q := u.Query()
	if len(q) == 0 {
		return "/"
	}
	if len(q) != 1 || len(q["flow"]) != 1 {
		return "/"
	}
	flow := q.Get("flow")
	if len(flow) == 0 || len(flow) > 15 {
		return "/"
	}
	for _, c := range flow {
		if c < '0' || c > '9' {
			return "/"
		}
	}
	return "/?flow=" + flow
}

func (a *App) ssoRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/auth/sso/config", func(w http.ResponseWriter, r *http.Request) {
		send(w, 200, Row{"enabled": a.ssoConfigured(), "loginUrl": "/auth/login"})
	})
	m.HandleFunc("GET /auth/login", a.ssoLogin)
	m.HandleFunc("GET /auth/oidc/callback", a.ssoCallback)
	m.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		a.checkRequestOrigin(r)
		a.logoutSession(w, r)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
	// The standalone Go server also serves the SPA login route in local previews.
	m.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		if a.Config.FrontendDir == "" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, strings.TrimRight(a.Config.FrontendDir, "/\\")+"/index.html")
	})
}

func (a *App) ssoLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(problem); ok {
				http.Redirect(w, r, "/login?sso_error=unavailable", http.StatusSeeOther)
			} else {
				panic(p)
			}
		}
	}()
	returnPath := ssoReturnPath(r.URL.Query().Get("return"))
	if a.resolve(r.Context(), a.cookieToken(r)) != nil {
		http.Redirect(w, r, returnPath, http.StatusSeeOther)
		return
	}
	_, cfg := a.ssoProvider(r.Context())
	state, browser, nonce, verifier := randomID(), randomID(), randomID(), oauth2.GenerateVerifier()
	a.sso.mu.Lock()
	if a.sso.attempts == nil {
		a.sso.attempts = map[string]ssoAttempt{}
	}
	for k, v := range a.sso.attempts {
		if time.Now().After(v.expires) {
			delete(a.sso.attempts, k)
		}
	}
	if len(a.sso.attempts) >= 2000 {
		a.sso.mu.Unlock()
		fail(429, "登录繁忙，请稍后重试")
	}
	a.sso.attempts[hash(state)] = ssoAttempt{browserHash: hash(browser), nonce: nonce, verifier: verifier, returnPath: returnPath, expires: time.Now().Add(10 * time.Minute)}
	a.sso.mu.Unlock()
	a.cookie(w, a.contextCookieName(), browser, 600)
	opts := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if r.URL.Query().Get("silent") == "1" {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "none"))
	}
	if a.Config.SSOProviderHint != "" {
		opts = append(opts, oauth2.SetAuthURLParam("provider_hint", a.Config.SSOProviderHint))
	}
	http.Redirect(w, r, cfg.AuthCodeURL(state, opts...), http.StatusSeeOther)
}

func (a *App) ssoCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	failure := func(reason string) {
		a.cookie(w, a.contextCookieName(), "", -1)
		http.Redirect(w, r, "/login?sso_error="+reason, http.StatusSeeOther)
	}
	defer func() {
		if p := recover(); p != nil {
			if v, ok := p.(problem); ok {
				if v.Status == 403 {
					failure("forbidden")
				} else {
					failure("unavailable")
				}
			} else {
				panic(p)
			}
		}
	}()
	q := r.URL.Query()
	state := q.Get("state")
	cookie, e := r.Cookie(a.contextCookieName())
	if e != nil || len(state) != 48 || len(q["state"]) != 1 {
		failure("state")
		return
	}
	a.sso.mu.Lock()
	attempt, ok := a.sso.attempts[hash(state)]
	if ok && subtle.ConstantTimeCompare([]byte(attempt.browserHash), []byte(hash(cookie.Value))) == 1 {
		delete(a.sso.attempts, hash(state))
	} else {
		ok = false
	}
	a.sso.mu.Unlock()
	if !ok || time.Now().After(attempt.expires) {
		failure("state")
		return
	}
	if q.Get("error") != "" {
		failure("cancelled")
		return
	}
	if q.Get("code") == "" || len(q.Get("code")) > 4096 || len(q["code"]) != 1 {
		failure("invalid")
		return
	}
	provider, cfg := a.ssoProvider(r.Context())
	ctx, cancel := context.WithTimeout(oidc.ClientContext(r.Context(), a.ssoClient()), 20*time.Second)
	defer cancel()
	token, err := cfg.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(attempt.verifier))
	if err != nil {
		failure("unavailable")
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 64<<10 {
		failure("invalid")
		return
	}
	id, err := provider.Verifier(&oidc.Config{ClientID: a.Config.SSOClientID, SupportedSigningAlgs: []string{oidc.RS256, oidc.RS512, oidc.ES256, oidc.ES384, oidc.ES512}}).Verify(ctx, raw)
	if err != nil || id.Subject == "" || len([]rune(id.Subject)) > 200 || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(attempt.nonce)) != 1 {
		failure("invalid")
		return
	}
	var claims struct {
		AZP         string `json:"azp"`
		DisplayName string `json:"displayName"`
		Name        string `json:"name"`
		NotBefore   int64  `json:"nbf"`
	}
	if id.Claims(&claims) != nil || (len(id.Audience) > 1 && claims.AZP == "") || (claims.AZP != "" && claims.AZP != a.Config.SSOClientID) || claims.NotBefore > time.Now().Add(time.Minute).Unix() || id.IssuedAt.IsZero() || id.IssuedAt.After(time.Now().Add(time.Minute)) || id.Expiry.IsZero() {
		failure("invalid")
		return
	}
	if id.AccessTokenHash != "" && id.VerifyAccessToken(token.AccessToken) != nil {
		failure("invalid")
		return
	}
	name := strings.TrimSpace(claims.DisplayName)
	if name == "" {
		name = strings.TrimSpace(claims.Name)
	}
	if len([]rune(name)) > 200 {
		name = ""
	}
	result := a.ssoSession(r.Context(), id.Issuer, id.Subject, name, a.requestToken(r))
	a.cookie(w, a.sessionCookieName(), str(result["token"]), 8*60*60)
	a.cookie(w, a.contextCookieName(), "", -1)
	http.Redirect(w, r, attempt.returnPath, http.StatusSeeOther)
}

func (a *App) ssoSession(ctx context.Context, issuer, subject, name, previous string) Row {
	return transaction(ctx, a.DB, func(tx *sql.Tx) Row {
		identity := one(ctx, tx, "SELECT user_id FROM external_identities WHERE issuer=? AND subject=? FOR UPDATE", issuer, subject)
		var id any
		if identity == nil {
			if name == "" {
				name = "统一登录用户"
			}
			// The owner explicitly enables ordinary employee access for all verified
			// SSO identities. Administrative roles still require controlled binding.
			id = insert(ctx, tx, "INSERT INTO users (username,password_hash,display_name,role) VALUES (?,?,?,'employee')", "sso_"+randomID(), "external:oidc", name)
			exec(ctx, tx, "INSERT INTO external_identities (issuer,subject,user_id) VALUES (?,?,?)", issuer, subject, id)
		} else {
			id = identity["user_id"]
		}
		u := one(ctx, tx, userJoin+"WHERE u.id=? AND "+validUser+" FOR UPDATE", id)
		if u == nil {
			fail(403, "此账号已停用或授权范围待核对，请联系管理员")
		}
		if previous != "" {
			exec(ctx, tx, "DELETE FROM sessions WHERE token_hash=?", hash(previous))
		}
		return a.session(ctx, tx, u)
	})
}

func (a *App) bindSSOIdentity(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	target := routeID(r, "id")
	source := integer(body(r)["sourceUserId"], 0, 9007199254740991)
	if target == source {
		fail(400, "请选择要关联的已有账号")
	}
	ctx := r.Context()
	transaction(ctx, a.DB, func(tx *sql.Tx) bool {
		// Consistent lock ordering also covers concurrent administrator changes.
		accounts := rows(ctx, tx, "SELECT id,role,active,employee_id FROM users WHERE id IN (?,?) ORDER BY id FOR UPDATE", target, source)
		if len(accounts) != 2 {
			fail(404, "待关联账号不存在")
		}
		var sourceUser, targetUser Row
		for _, u := range accounts {
			if num(u["id"]) == source {
				sourceUser = u
			} else {
				targetUser = u
			}
		}
		if !contains([]string{"guest", "employee"}, str(sourceUser["role"])) || sourceUser["employee_id"] != nil || num(sourceUser["active"]) != 1 || num(targetUser["active"]) != 1 {
			fail(409, "仅可将尚未关联员工的普通登录身份绑定到启用中的已有账号")
		}
		if exists(ctx, tx, "SELECT id FROM asset_flows WHERE applicant_id=? LIMIT 1", source) || exists(ctx, tx, "SELECT id FROM asset_flow_steps WHERE approver_id=? LIMIT 1", source) {
			fail(409, "来源账号已有业务记录，请先核对员工关联，不能直接合并账号")
		}
		identity := one(ctx, tx, "SELECT issuer,subject FROM external_identities WHERE user_id=? FOR UPDATE", source)
		if identity == nil || exists(ctx, tx, "SELECT user_id FROM external_identities WHERE user_id=?", target) {
			fail(409, "统一登录身份已变化，请刷新后重试")
		}
		exec(ctx, tx, "UPDATE external_identities SET user_id=? WHERE user_id=?", target, source)
		exec(ctx, tx, "UPDATE users SET active=0,updated_at=CURRENT_TIMESTAMP(3) WHERE id=?", source)
		exec(ctx, tx, "DELETE FROM sessions WHERE user_id IN (?,?)", source, target)
		audit(ctx, tx, actor(r), "bind_sso_identity", str(target), Row{"sourceUserId": source, "targetUserId": target})
		return true
	})
	send(w, 200, Row{"status": "bound"})
}
