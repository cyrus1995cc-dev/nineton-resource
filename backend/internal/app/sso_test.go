package app

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

type fakeOIDC struct {
	server                    *httptest.Server
	key                       *rsa.PrivateKey
	override                  Row
	nonce, challenge, subject string
	exchanges                 int
}

func mockOIDC(t *testing.T, a *App) *fakeOIDC {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	f := &fakeOIDC{key: key, subject: "employee-subject"}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			send(w, 200, Row{"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/authorize", "token_endpoint": f.server.URL + "/token", "jwks_uri": f.server.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic"}})
		case "/keys":
			send(w, 200, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			f.exchanges++
			client, secret, ok := r.BasicAuth()
			if r.Method != "POST" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") || !ok || client != "asset-client" || secret != "server-only-secret" {
				t.Error("incorrect token authentication")
				http.Error(w, "invalid_client", 401)
				return
			}
			if r.ParseForm() != nil || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != a.Config.SSORedirectURI {
				t.Error("incorrect token request")
				http.Error(w, "invalid_request", 400)
				return
			}
			challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(challenge[:]) != f.challenge {
				t.Error("PKCE verifier mismatch")
				http.Error(w, "invalid_grant", 400)
				return
			}
			claims := Row{"iss": f.server.URL, "sub": f.subject, "aud": "asset-client", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": f.nonce, "displayName": "系统管理员", "email": "admin@example.com", "isAdmin": true}
			for k, v := range f.override {
				claims[k] = v
			}
			signer, e := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"))
			if e != nil {
				t.Fatal(e)
			}
			payload, _ := json.Marshal(claims)
			signed, e := signer.Sign(payload)
			if e != nil {
				t.Fatal(e)
			}
			raw, e := signed.CompactSerialize()
			if e != nil {
				t.Fatal(e)
			}
			if r.Form.Get("code") == "forged" {
				raw = raw[:len(raw)-12] + "badSignature"
			}
			send(w, 200, Row{"access_token": "do-not-persist", "token_type": "Bearer", "id_token": raw, "expires_in": 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	a.Config.SSOIssuer = f.server.URL
	a.Config.SSOClientID = "asset-client"
	a.Config.SSOClientSecret = "server-only-secret"
	a.Config.SSORedirectURI = "http://127.0.0.1/auth/oidc/callback"
	return f
}

func oidcStart(t *testing.T, a *App, f *fakeOIDC, path string) (string, *http.Cookie) {
	t.Helper()
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != 303 {
		t.Fatalf("login start: %d %s", w.Code, w.Body.String())
	}
	u, e := url.Parse(w.Header().Get("Location"))
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	f.nonce = q.Get("nonce")
	f.challenge = q.Get("code_challenge")
	if q.Get("response_type") != "code" || q.Get("client_id") != "asset-client" || q.Get("code_challenge_method") != "S256" || f.nonce == "" || f.challenge == "" || q.Get("client_secret") != "" {
		t.Fatal("unsafe authorization request")
	}
	cs := w.Result().Cookies()
	if len(cs) != 1 || !cs[0].HttpOnly || cs[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("login context cookie")
	}
	return q.Get("state"), cs[0]
}

func oidcCallback(a *App, state, code string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/auth/oidc/callback?"+url.Values{"state": {state}, "code": {code}}.Encode(), nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func cookieRequest(a *App, cookie *http.Cookie, method, path, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.AddCookie(cookie)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func TestOIDCProvisioningAndControlledBinding(t *testing.T) {
	a, admin := testApp(t)
	f := mockOIDC(t, a)
	ctx := context.Background()
	state, contextCookie := oidcStart(t, a, f, "/auth/login?return=%2F%3Fflow%3D123")
	w := oidcCallback(a, state, "good", contextCookie)
	if w.Code != 303 || w.Header().Get("Location") != "/?flow=123" {
		t.Fatalf("callback %d %s", w.Code, w.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == a.sessionCookieName() {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.Path != "/" || cookie.MaxAge != 28800 || strings.Contains(w.Header().Get("Location"), "token") {
		t.Fatal("unsafe app session")
	}
	guest := one(ctx, a.DB, userJoin+"WHERE u.username LIKE 'sso_%'")
	if guest == nil || guest["role"] != "employee" || guest["employee_id"] != nil || VerifyPassword("do-not-persist", str(guest["password_hash"])) {
		t.Fatal("first login must be a separate ordinary employee")
	}
	for _, path := range []string{"/api/directory/status", "/api/directory/mappings", "/api/companies", "/api/auth/users", "/api/inventory/tasks"} {
		if result := cookieRequest(a, cookie, "GET", path, ""); result.Code != 403 {
			t.Fatalf("guest accessed %s: %d", path, result.Code)
		}
	}
	// No name-based account, employee or historical-asset matching. New staff
	// can browse available stock but cannot inspect another employee's assets.
	seed(ctx, a.DB, "SSO-IDLE", "空闲")
	seed(ctx, a.DB, "SSO-PRIVATE", "在用")
	result := cookieRequest(a, cookie, "GET", "/api/assets?scope=available", "")
	if result.Code != 200 || !strings.Contains(result.Body.String(), "SSO-IDLE") || strings.Contains(result.Body.String(), "SSO-PRIVATE") {
		t.Fatal("ordinary employee available asset scope")
	}
	result = cookieRequest(a, cookie, "GET", "/api/assets?scope=mine", "")
	if result.Code != 200 || strings.Contains(result.Body.String(), "SSO-PRIVATE") {
		t.Fatal("ordinary employee historical assets were matched by display name")
	}
	// An unused ordinary SSO account can still be explicitly bound by an
	// administrator to retain an existing employee's role and business identity.
	if result := cookieRequest(a, cookie, "GET", "/api/auth/me", ""); result.Code != 200 {
		t.Fatal("guest cannot inspect own authorization state")
	}
	if result := oidcCallback(a, state, "good", contextCookie); result.Code != 303 || !strings.Contains(result.Header().Get("Location"), "sso_error=state") || f.exchanges != 1 {
		t.Fatal("replayed callback")
	}
	// Names and upstream isAdmin never merge the guest into this local admin.
	adminID := num(one(ctx, a.DB, "SELECT id FROM users WHERE username='admin'")["id"])
	admin.call("POST", "/api/auth/users/"+str(adminID)+"/sso-binding", Row{"sourceUserId": guest["id"]}, 200)
	if a.resolve(ctx, admin.token) != nil || a.resolve(ctx, cookie.Value) != nil {
		t.Fatal("binding did not revoke both accounts' sessions")
	}
	if one(ctx, a.DB, "SELECT active FROM users WHERE id=?", guest["id"])["active"] != int64(0) {
		t.Fatal("orphaned guest still active")
	}
	state, contextCookie = oidcStart(t, a, f, "/auth/login")
	w = oidcCallback(a, state, "good", contextCookie)
	if w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatal("bound login failed")
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == a.sessionCookieName() {
			cookie = c
		}
	}
	u := a.resolve(ctx, cookie.Value)
	if num(u["id"]) != adminID || u["role"] != "admin" {
		t.Fatal("binding lost existing role")
	}
	if num(one(ctx, a.DB, "SELECT COUNT(*) n FROM external_identities")["n"]) != 1 {
		t.Fatal("duplicate external identity")
	}
	if result := cookieRequest(a, cookie, "POST", "/api/auth/logout", "https://evil.example"); result.Code != 403 || a.resolve(ctx, cookie.Value) == nil {
		t.Fatal("logout CSRF")
	}
	if result := cookieRequest(a, cookie, "POST", "/api/auth/logout", "http://127.0.0.1"); result.Code != 204 || a.resolve(ctx, cookie.Value) != nil {
		t.Fatal("local logout did not revoke server session")
	}
	if result := cookieRequest(a, cookie, "POST", "/api/auth/logout", "http://127.0.0.1"); result.Code != 204 {
		t.Fatal("logout is not idempotent")
	}
	exec(ctx, a.DB, "UPDATE users SET active=0 WHERE id=?", adminID)
	state, contextCookie = oidcStart(t, a, f, "/auth/login")
	if result := oidcCallback(a, state, "good", contextCookie); result.Header().Get("Location") != "/login?sso_error=forbidden" {
		t.Fatal("disabled account can log in again")
	}
}

func TestOIDCRejectsUntrustedCallbacks(t *testing.T) {
	a, _ := testApp(t)
	f := mockOIDC(t, a)
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		claims Row
		code   string
	}{
		{"nonce", Row{"nonce": "wrong"}, "good"},
		{"issuer", Row{"iss": "https://evil.example"}, "good"},
		{"audience", Row{"aud": "other-app"}, "good"},
		{"expiry", Row{"exp": time.Now().Add(-time.Minute).Unix()}, "good"},
		{"not-before", Row{"nbf": time.Now().Add(time.Hour).Unix()}, "good"},
		{"multi-audience-without-azp", Row{"aud": []string{"asset-client", "other-app"}}, "good"},
		{"wrong-azp", Row{"azp": "other-app"}, "good"},
		{"empty-subject", Row{"sub": ""}, "good"},
		{"future-issued-at", Row{"iat": time.Now().Add(time.Hour).Unix()}, "good"},
		{"signature", nil, "forged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.override = tc.claims
			state, cookie := oidcStart(t, a, f, "/auth/login")
			w := oidcCallback(a, state, tc.code, cookie)
			if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "sso_error=invalid") {
				t.Fatalf("accepted invalid identity: %d %s", w.Code, w.Header().Get("Location"))
			}
		})
	}
	f.override = nil
	state, cookie := oidcStart(t, a, f, "/auth/login")
	for _, badCookie := range []*http.Cookie{nil, {Name: cookie.Name, Value: "wrong-browser"}} {
		w := oidcCallback(a, state, "good", badCookie)
		if !strings.Contains(w.Header().Get("Location"), "sso_error=state") {
			t.Fatal("browser binding bypass")
		}
	}
	a.sso.mu.Lock()
	attempt := a.sso.attempts[hash(state)]
	attempt.expires = time.Now().Add(-time.Second)
	a.sso.attempts[hash(state)] = attempt
	a.sso.mu.Unlock()
	if w := oidcCallback(a, state, "good", cookie); !strings.Contains(w.Header().Get("Location"), "sso_error=state") {
		t.Fatal("expired context")
	}
	if exists(ctx, a.DB, "SELECT user_id FROM external_identities") {
		t.Fatal("failed callback created an identity")
	}
}

func TestOIDCCancelledAndIdleExpiry(t *testing.T) {
	a, _ := testApp(t)
	f := mockOIDC(t, a)
	state, cookie := oidcStart(t, a, f, "/auth/login?silent=1")
	r := httptest.NewRequest("GET", "/auth/oidc/callback?error=login_required&state="+state, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 303 || w.Header().Get("Location") != "/login?sso_error=cancelled" || f.exchanges != 0 {
		t.Fatal("silent login error loops or exchanges code")
	}
	state, cookie = oidcStart(t, a, f, "/auth/login")
	w = oidcCallback(a, state, "good", cookie)
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == a.sessionCookieName() {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session")
	}
	exec(context.Background(), a.DB, "UPDATE session_policies SET idle_until=? WHERE token_hash=?", time.Now().Add(-time.Second).UnixMilli(), hash(session.Value))
	if result := cookieRequest(a, session, "GET", "/api/auth/me", ""); result.Code != 401 {
		t.Fatal("idle expired session still valid")
	}
}

func TestSSOReturnAndHTTPSCookies(t *testing.T) {
	for _, path := range []string{"https://evil.example/", "//evil.example", "/\\evil.example", "/%2f%2fevil.example", "/auth/login", "/?flow=1&next=https://evil.example", "/?flow=1&flow=2", "/?flow=abc"} {
		if ssoReturnPath(path) != "/" {
			t.Fatalf("unsafe return %s", path)
		}
	}
	a := New(nil, Config{SSORedirectURI: "https://xz.vodayapp.com/auth/oidc/callback"})
	w := httptest.NewRecorder()
	a.cookie(w, a.sessionCookieName(), randomID(), 28800)
	c := w.Result().Cookies()[0]
	if !c.Secure || !c.HttpOnly || c.Domain != "" || c.Path != "/" || !strings.HasPrefix(c.Name, "__Host-") {
		t.Fatal("unsafe HTTPS session cookie")
	}
	r := httptest.NewRequest("GET", "https://xz.vodayapp.com/api/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: "asset_session", Value: randomID()})
	if a.cookieToken(r) != "" {
		t.Fatal("HTTPS accepts an insecure cookie name")
	}
}

func TestOIDCKeyRotationAndRepeatedIdentity(t *testing.T) {
	a, _ := testApp(t)
	f := mockOIDC(t, a)
	state, cookie := oidcStart(t, a, f, "/auth/login")
	if result := oidcCallback(a, state, "good", cookie); result.Header().Get("Location") != "/" {
		t.Fatal("first login")
	}
	newKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	f.key = newKey
	state, cookie = oidcStart(t, a, f, "/auth/login")
	if result := oidcCallback(a, state, "good", cookie); result.Header().Get("Location") != "/" {
		t.Fatal("JWKS key rotation failed")
	}
	if num(one(context.Background(), a.DB, "SELECT COUNT(*) n FROM external_identities")["n"]) != 1 || num(one(context.Background(), a.DB, "SELECT COUNT(*) n FROM users WHERE role='employee'")["n"]) != 1 {
		t.Fatal("repeat login created duplicate accounts")
	}
}
