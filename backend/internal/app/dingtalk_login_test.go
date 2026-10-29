package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMySQLDingTalkEnterpriseLoginIdentity(t *testing.T) {
	a, admin := testApp(t)
	ctx := context.Background()
	a.Config.DingID, a.Config.DingSecret, a.Config.DingCorp = "app", "private-secret", "corp-a"
	var exchanges atomic.Int32
	var appTokens atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			appTokens.Add(1)
			send(w, 200, Row{"accessToken": "enterprise-token", "expireIn": 7200})
		case "/topapi/v2/user/getuserinfo":
			exchanges.Add(1)
			if r.URL.Query().Get("access_token") != "enterprise-token" {
				t.Error("enterprise app token was not supplied")
			}
			code := str(body(r)["code"])
			switch code {
			case "invalid-code":
				send(w, 200, Row{"errcode": 40029})
			case "empty-identity":
				send(w, 200, Row{"errcode": 0, "result": Row{"name": "同名"}})
			default:
				// Enterprise administrator flags and a matching name grant no local privileges.
				send(w, 200, Row{"errcode": 0, "result": Row{"userid": code, "name": "同名", "sys": true, "sys_level": 1}})
			}
		default:
			t.Errorf("unexpected OAuth request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer mock.Close()
	a.Config.DingAPI, a.Config.DingOAPI = mock.URL, mock.URL
	employeeID := insert(ctx, a.DB, "INSERT INTO employees (name,corp_id,dingtalk_user_id) VALUES ('同名','corp-a','employee-a')")
	insert(ctx, a.DB, "INSERT INTO employees (name,corp_id,dingtalk_user_id) VALUES ('同名','corp-b','other-corp-employee')")
	anon := testClient{t: t, handler: a.Handler()}
	config := anon.call("GET", "/api/auth/dingtalk/config", nil, 200)
	if config["clientId"] != "app" || config["corpId"] != "corp-a" || strings.Contains(js(config), "private-secret") {
		t.Fatal("invalid public app configuration", config)
	}
	anon.call("POST", "/api/auth/dingtalk/login", Row{"code": ""}, 400)
	if exchanges.Load() != 0 {
		t.Fatal("empty code reached DingTalk")
	}
	anon.call("POST", "/api/auth/dingtalk/login", Row{"code": "invalid-code"}, 502)
	anon.call("POST", "/api/auth/dingtalk/login", Row{"code": "empty-identity"}, 502)
	anon.call("POST", "/api/auth/dingtalk/login", Row{"code": "unrecognized-employee"}, 403)
	anon.call("POST", "/api/auth/dingtalk/login", Row{"code": "other-corp-employee"}, 403)
	login := anon.call("POST", "/api/auth/dingtalk/login", Row{"code": "employee-a"}, 200)
	user := login["user"].(map[string]any)
	if user["role"] != "employee" || num(user["employee_id"]) != employeeID {
		t.Fatal("identity or default privilege mismatch", user)
	}
	exec(ctx, a.DB, "UPDATE users SET role='approver' WHERE employee_id=?", employeeID)
	login = anon.call("POST", "/api/auth/dingtalk/login", Row{"code": "employee-a"}, 200)
	if login["user"].(map[string]any)["role"] != "approver" {
		t.Fatal("existing local role was overwritten")
	}
	exec(ctx, a.DB, "UPDATE users SET active=0 WHERE employee_id=?", employeeID)
	anon.call("POST", "/api/auth/dingtalk/login", Row{"code": "employee-a"}, 403)
	exec(ctx, a.DB, "UPDATE users SET active=1 WHERE employee_id=?", employeeID)
	exec(ctx, a.DB, "UPDATE employees SET missing_since=CURRENT_TIMESTAMP(3) WHERE id=?", employeeID)
	anon.call("POST", "/api/auth/dingtalk/login", Row{"code": "employee-a"}, 403)
	exec(ctx, a.DB, "UPDATE employees SET missing_since=NULL,active=0 WHERE id=?", employeeID)
	anon.call("POST", "/api/auth/dingtalk/login", Row{"code": "employee-a"}, 403)
	if appTokens.Load() != 1 {
		t.Fatal("enterprise token not cached", appTokens.Load())
	}
	// DingTalk login must not affect the local administrator recovery account.
	admin.call("GET", "/api/auth/me", nil, 200)
}
