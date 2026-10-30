package app

import (
	"context"
	"database/sql"
	"log/slog"
)

// This is an explicit issuer contract, never a guess based on names or emails.
// The OIDC callback verifies the signature, issuer and audience before this runs.
func (a *App) ssoDirectoryUser(ctx context.Context, tx *sql.Tx, issuer, subject string, current any) any {
	if !a.Config.SSOSubjectIsDingTalkUserID || issuer != a.Config.SSOIssuer || a.Config.DingCorp == "" {
		return current
	}
	employee := one(ctx, tx, "SELECT id,active,missing_since FROM employees WHERE corp_id=? AND dingtalk_user_id=? FOR UPDATE", a.Config.DingCorp, subject)
	if employee == nil {
		return current
	}
	if num(employee["active"]) != 1 || employee["missing_since"] != nil {
		fail(403, "此员工已停用或通讯录状态待核对，请联系管理员")
	}
	employeeID := employee["id"]
	if current == nil {
		ensureEmployeeUser(ctx, tx, employeeID)
		target := one(ctx, tx, "SELECT * FROM users WHERE employee_id=? FOR UPDATE", employeeID)
		// An unbound login must never acquire an existing privileged account.
		if target == nil || target["role"] != "employee" || num(target["active"]) != 1 || exists(ctx, tx, "SELECT user_id FROM external_identities WHERE user_id=?", target["id"]) {
			fail(409, "此员工的登录账号需管理员核对后关联")
		}
		audit(ctx, tx, nil, "sso_directory_link", str(target["id"]), Row{"employeeId": employeeID, "method": "issuer_subject_dingtalk_id"})
		return target["id"]
	}
	source := one(ctx, tx, "SELECT * FROM users WHERE id=? FOR UPDATE", current)
	if source == nil || num(source["active"]) != 1 {
		fail(403, "此账号已停用，请联系管理员")
	}
	if source["employee_id"] != nil {
		if num(source["employee_id"]) != num(employeeID) {
			fail(409, "登录身份与已有员工关联不一致，请联系管理员核对")
		}
		return current
	}
	placeholder := one(ctx, tx, "SELECT * FROM users WHERE employee_id=? FOR UPDATE", employeeID)
	if placeholder != nil {
		// Preserve the established SSO account, role, sessions and business history.
		// Only retire the unused ordinary account precreated by directory sync.
		if placeholder["role"] != "employee" || placeholder["password_hash"] != "external:dingtalk" || num(placeholder["active"]) != 1 || directoryAccountUsed(ctx, tx, placeholder["id"]) {
			fail(409, "此员工已有其他登录或业务账号，请联系管理员核对")
		}
		exec(ctx, tx, "UPDATE users SET employee_id=NULL,active=0,updated_at=CURRENT_TIMESTAMP(3) WHERE id=?", placeholder["id"])
	}
	exec(ctx, tx, "UPDATE users SET employee_id=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=?", employeeID, current)
	details := Row{"employeeId": employeeID, "method": "issuer_subject_dingtalk_id"}
	if placeholder != nil {
		details["retiredDirectoryUserId"] = placeholder["id"]
	}
	audit(ctx, tx, nil, "sso_directory_link", str(current), details)
	return current
}

func directoryAccountUsed(ctx context.Context, tx *sql.Tx, id any) bool {
	for _, ref := range []struct{ table, column string }{
		{"external_identities", "user_id"}, {"sessions", "user_id"},
		{"asset_flows", "applicant_id"}, {"asset_flow_steps", "approver_id"},
		{"asset_flow_logs", "actor_id"}, {"asset_flow_comments", "user_id"},
		{"notification_outbox", "recipient_id"}, {"asset_create_requests", "actor_id"},
		{"import_previews", "actor_id"}, {"audit_logs", "actor_id"}, {"directory_sync_runs", "actor_id"},
	} {
		if exists(ctx, tx, "SELECT 1 FROM "+ref.table+" WHERE "+ref.column+"=? LIMIT 1", id) {
			return true
		}
	}
	return false
}

// Repair prior logins at startup so existing sessions immediately see the same
// employee ownership as future logins. A conflict is left for controlled review.
func (a *App) ReconcileSSODirectory(ctx context.Context) {
	if !a.Config.SSOSubjectIsDingTalkUserID {
		return
	}
	if a.Config.SSOIssuer == "" || a.Config.DingCorp == "" {
		fail(500, "自动关联统一登录员工需要配置 SSO_ISSUER 和 DINGTALK_CORP_ID")
	}
	for _, identity := range rows(ctx, a.DB, "SELECT x.subject,x.user_id FROM external_identities x JOIN users u ON u.id=x.user_id WHERE x.issuer=? AND u.active=1 AND u.employee_id IS NULL ORDER BY x.user_id", a.Config.SSOIssuer) {
		func() {
			defer func() {
				if p := recover(); p != nil {
					if issue, ok := p.(problem); ok && (issue.Status == 403 || issue.Status == 409) {
						slog.Warn("SSO directory link requires review", "user_id", identity["user_id"], "reason", issue.Message)
						return
					}
					panic(p)
				}
			}()
			transaction(ctx, a.DB, func(tx *sql.Tx) bool {
				// Recheck the binding under lock in case an administrator changed it.
				locked := one(ctx, tx, "SELECT user_id FROM external_identities WHERE issuer=? AND subject=? FOR UPDATE", a.Config.SSOIssuer, identity["subject"])
				if locked != nil && num(locked["user_id"]) == num(identity["user_id"]) {
					a.ssoDirectoryUser(ctx, tx, a.Config.SSOIssuer, str(identity["subject"]), locked["user_id"])
				}
				return true
			})
		}()
	}
}
