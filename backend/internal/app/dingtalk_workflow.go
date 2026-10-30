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
	"strings"
	"time"
)

func (a *App) oaEnabled() bool {
	return a.Config.DingApprovalEnabled && a.dingConfigured() && a.Config.DingProcessCode != "" && a.Config.DingAgentID > 0 && a.Config.FixedApproverID > 0
}

func (a *App) publicURL() string {
	u, err := url.Parse(a.Config.PublicURL)
	if err != nil || u.Host == "" || u.Scheme != "https" {
		return "https://xz.vodayapp.com"
	}
	return u.Scheme + "://" + u.Host
}

func (a *App) workflowCall(ctx context.Context, method, path string, payload any) Row {
	var input io.Reader
	if payload != nil {
		input = bytes.NewBufferString(js(payload))
	}
	req, err := http.NewRequestWithContext(ctx, method, a.dingBase()+path, input)
	must(err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", a.dingToken(ctx))
	res, err := a.client.Do(req)
	if err != nil {
		fail(504, "钉钉请求结果不确定，请先核对审批记录，勿重复提交")
	}
	defer res.Body.Close()
	var data Row
	d := json.NewDecoder(io.LimitReader(res.Body, 2<<20))
	d.UseNumber()
	if d.Decode(&data) != nil || data == nil {
		fail(504, "钉钉响应无法确认，请核对审批记录")
	}
	if res.StatusCode >= 300 {
		code := str(data["code"])
		if res.StatusCode >= 500 {
			fail(504, "钉钉服务响应异常，请核对审批记录")
		}
		if len(code) > 100 {
			code = "request_rejected"
		}
		fail(502, "钉钉接口拒绝请求（"+code+"），请核对审批权限和模板")
	}
	return data
}

func (a *App) queueOA(ctx context.Context, tx *sql.Tx, f Row) {
	if num(f["applicant_id"]) == a.Config.FixedApproverID && a.Config.FixedApproverID > 0 {
		u := a.fixedApprover(ctx, tx)
		a.processFlowAction(ctx, tx, num(f["id"]), "approve", "资产管理员直接办理", "", u)
		audit(ctx, tx, u, "admin_direct_handling", str(f["id"]), Row{"type": f["type"]})
		return
	}
	if !a.oaEnabled() {
		return
	}
	steps := rows(ctx, tx, "SELECT approver_id FROM asset_flow_steps WHERE flow_id=? ORDER BY sequence", f["id"])
	if len(steps) != 1 || num(steps[0]["approver_id"]) != a.Config.FixedApproverID {
		fail(409, "钉钉审批只支持固定资产管理员")
	}
	identity := func(id any) string {
		e := one(ctx, tx, "SELECT e.dingtalk_user_id,e.corp_id FROM users u JOIN employees e ON e.id=u.employee_id WHERE u.id=? AND u.active=1 AND e.active=1 AND e.missing_since IS NULL", id)
		if e == nil || str(e["corp_id"]) != a.Config.DingCorp || str(e["dingtalk_user_id"]) == "" {
			fail(409, "申请人或管理员未关联当前企业钉钉员工身份")
		}
		return str(e["dingtalk_user_id"])
	}
	exec(ctx, tx, "INSERT INTO workflow_oa(flow_id,process_code,originator_user_id,approver_user_id) VALUES (?,?,?,?)", f["id"], a.Config.DingProcessCode, identity(f["applicant_id"]), identity(steps[0]["approver_id"]))
}

func workflowProblem(p any) (string, string) {
	if issue, ok := p.(problem); ok {
		if issue.Status == 504 {
			return "uncertain", issue.Message
		}
		return "failed", issue.Message
	}
	return "uncertain", "处理状态需核对，请刷新单据后联系管理员"
}

func (a *App) syncOneApproval(ctx context.Context, id int64) {
	var oa Row
	defer func() {
		if p := recover(); p != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			state, message := workflowProblem(p)
			if oa != nil && oa["instance_id"] != nil {
				state = "active"
			}
			exec(cleanup, a.DB, "UPDATE workflow_oa SET state=?,last_error=?,next_attempt=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 60 SECOND),checked_at=CURRENT_TIMESTAMP(3) WHERE flow_id=?", state, message, id)
			slog.Warn("DingTalk approval sync needs retry", "flow_id", id, "state", state)
		}
	}()
	oa = one(ctx, a.DB, "SELECT * FROM workflow_oa WHERE flow_id=?", id)
	if oa == nil {
		return
	}
	f := one(ctx, a.DB, "SELECT * FROM asset_flows WHERE id=?", id)
	if f == nil {
		return
	}
	if oa["instance_id"] == nil {
		if f["status"] != "pending" {
			exec(ctx, a.DB, "UPDATE workflow_oa SET state='done' WHERE flow_id=?", id)
			return
		}
		if oa["state"] != "queued" {
			return
		}
		// Persist before the network call. A crash or timeout requires reconciliation,
		// never a blind retry that could create duplicate approval instances.
		if n, _ := exec(ctx, a.DB, "UPDATE workflow_oa SET state='creating',attempts=attempts+1,last_error='' WHERE flow_id=? AND state='queued'", id).RowsAffected(); n != 1 {
			return
		}
		demand := one(ctx, a.DB, "SELECT * FROM asset_requisitions WHERE flow_id=?", id)
		items := rows(ctx, a.DB, "SELECT asset_code,asset_name FROM asset_flow_items WHERE flow_id=? ORDER BY id", id)
		lines := []string{}
		for _, item := range items {
			lines = append(lines, str(item["asset_code"])+" · "+str(item["asset_name"]))
		}
		content := str(f["note"])
		if demand != nil {
			content = fmt.Sprintf("%s，%d件，需求日期：%s\n%s", str(demand["category_path"]), num(demand["quantity"]), str(demand["needed_date"]), str(demand["purpose"]))
		}
		for _, field := range []struct{ key, label string }{{"applicant_department", "申请部门"}, {"to_assignee", "目标使用人"}, {"to_department", "目标部门"}, {"to_location", "目标位置"}} {
			if value := str(f[field.key]); value != "" {
				content += "\n" + field.label + "：" + value
			}
		}
		if len(lines) == 0 {
			lines = []string{"按设备类别申请，由管理员在九吨资产分配具体设备。"}
		}
		content += fmt.Sprintf("\n查看单据：%s/?flow=%d", a.publicURL(), id)
		department := one(ctx, a.DB, "SELECT d.dingtalk_id FROM users u JOIN employee_departments ed ON ed.employee_id=u.employee_id JOIN departments d ON d.id=ed.department_id WHERE u.id=? AND d.corp_id=? ORDER BY d.dingtalk_id LIMIT 1", f["applicant_id"], a.Config.DingCorp)
		dept := int64(1)
		if department != nil {
			dept = num(department["dingtalk_id"])
		}
		values := []Row{{"name": "系统单号", "value": f["document_no"]}, {"name": "申请类型", "value": f["type"]}, {"name": "申请内容", "value": content}, {"name": "资产清单", "value": strings.Join(lines, "\n")}}
		// bizDetailPageUrl requires the paid OA edition. A normal form link works
		// with the enterprise's standard native approval service.
		result := a.workflowCall(ctx, "POST", "/v1.0/workflow/processInstances", Row{"processCode": oa["process_code"], "originatorUserId": oa["originator_user_id"], "deptId": dept, "microappAgentId": a.Config.DingAgentID, "approvers": []Row{{"actionType": "NONE", "userIds": []string{str(oa["approver_user_id"])}}}, "formComponentValues": values})
		instance := str(result["instanceId"])
		if instance == "" || len(instance) > 200 {
			fail(504, "钉钉未返回可确认的审批编号")
		}
		exec(ctx, a.DB, "UPDATE workflow_oa SET instance_id=?,state='active',last_error='',checked_at=CURRENT_TIMESTAMP(3) WHERE flow_id=?", instance, id)
		exec(ctx, a.DB, "UPDATE notification_outbox SET status='sent',sent_at=CURRENT_TIMESTAMP(3) WHERE flow_id=? AND kind='approval_pending' AND status='pending'", id)
		oa["instance_id"] = instance
	}
	result := a.workflowCall(ctx, "GET", "/v1.0/workflow/processInstances?processInstanceId="+url.QueryEscape(str(oa["instance_id"])), nil)
	detail, ok := result["result"].(map[string]any)
	if !ok {
		fail(502, "钉钉未返回审批详情")
	}
	if str(detail["originatorUserId"]) != str(oa["originator_user_id"]) {
		fail(409, "钉钉审批发起人不匹配，已停止回写")
	}
	state, decision := strings.ToUpper(str(detail["status"])), strings.ToLower(str(detail["result"]))
	if f["status"] == "withdrawn" {
		message := ""
		if state == "COMPLETED" {
			message = "钉钉审批已结束；系统申请已撤回，未按审批变更资产"
		} else if state != "TERMINATED" {
			terminated := a.workflowCall(ctx, "POST", "/v1.0/workflow/processInstances/terminate", Row{"processInstanceId": oa["instance_id"], "operatingUserId": oa["originator_user_id"], "isSystem": true, "remark": "申请人在九吨资产撤回"})
			if terminated["success"] != true || terminated["result"] != true {
				fail(502, "钉钉撤回尚未确认，请稍后同步")
			}
		}
		exec(ctx, a.DB, "UPDATE workflow_oa SET state='done',last_error=?,checked_at=CURRENT_TIMESTAMP(3) WHERE flow_id=?", message, id)
		return
	}
	if state == "COMPLETED" && (decision == "agree" || decision == "refuse") {
		verified := false
		comment := "钉钉审批结果同步"
		if records, ok := detail["operationRecords"].([]any); ok {
			for _, raw := range records {
				record, _ := raw.(map[string]any)
				if str(record["userId"]) == str(oa["approver_user_id"]) && strings.EqualFold(str(record["result"]), map[string]string{"agree": "AGREE", "refuse": "REFUSE"}[decision]) {
					verified = true
					if s := str(record["remark"]); s != "" {
						comment = text(s, 2000, false)
					}
				}
			}
		}
		if !verified {
			fail(409, "未取得指定管理员的审批记录，已停止回写")
		}
		transaction(ctx, a.DB, func(tx *sql.Tx) bool {
			f := one(ctx, tx, "SELECT * FROM asset_flows WHERE id=? FOR UPDATE", id)
			if f["status"] == "pending" {
				u := one(ctx, tx, userJoin+"JOIN asset_flow_steps s ON s.approver_id=u.id WHERE s.flow_id=? AND s.sequence=1 AND u.role='admin' AND "+validUser, id)
				if u == nil {
					fail(409, "固定管理员已停用，无法回写审批")
				}
				identity := one(ctx, tx, "SELECT corp_id,dingtalk_user_id FROM employees WHERE id=? AND active=1 AND missing_since IS NULL", u["employee_id"])
				if identity == nil || str(identity["corp_id"]) != a.Config.DingCorp || str(identity["dingtalk_user_id"]) != str(oa["approver_user_id"]) {
					fail(409, "指定管理员身份已变化，已停止回写")
				}
				action := "approve"
				if decision == "refuse" {
					action = "reject"
				}
				a.processFlowAction(ctx, tx, id, action, comment, "", publicUser(u))
			}
			exec(ctx, tx, "UPDATE workflow_oa SET state='done',last_error='',checked_at=CURRENT_TIMESTAMP(3) WHERE flow_id=?", id)
			return true
		})
	} else if state == "TERMINATED" {
		transaction(ctx, a.DB, func(tx *sql.Tx) bool {
			f := one(ctx, tx, "SELECT * FROM asset_flows WHERE id=? FOR UPDATE", id)
			if f["status"] == "pending" {
				u := one(ctx, tx, "SELECT * FROM users WHERE id=?", f["applicant_id"])
				a.processFlowAction(ctx, tx, id, "withdraw", "钉钉审批已撤销", "", u)
			}
			exec(ctx, tx, "UPDATE workflow_oa SET state='done',last_error='',checked_at=CURRENT_TIMESTAMP(3) WHERE flow_id=?", id)
			return true
		})
	} else {
		exec(ctx, a.DB, "UPDATE workflow_oa SET checked_at=CURRENT_TIMESTAMP(3),last_error='',next_attempt=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 30 SECOND) WHERE flow_id=?", id)
	}
}

func (a *App) syncApprovalHTTP(w http.ResponseWriter, r *http.Request) {
	id := routeID(r, "id")
	if !canViewFlow(r.Context(), a.DB, id, actor(r)) {
		fail(404, "单据不存在或无权查看")
	}
	if !a.oaEnabled() {
		fail(503, "钉钉审批未启用")
	}
	if !a.workflowMu.TryLock() {
		fail(429, "正在同步其他审批，请稍后刷新")
	}
	defer a.workflowMu.Unlock()
	a.syncOneApproval(r.Context(), id)
	send(w, 200, flowDetail(r.Context(), a.DB, id))
}

func (a *App) retryApprovalHTTP(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	id, ctx := routeID(r, "id"), r.Context()
	if num(actor(r)["id"]) != a.Config.FixedApproverID {
		fail(403, "只有固定管理员可重试")
	}
	if !a.oaEnabled() {
		fail(503, "钉钉审批未启用")
	}
	if n, _ := exec(ctx, a.DB, "UPDATE workflow_oa SET state='queued',last_error='' WHERE flow_id=? AND instance_id IS NULL AND state='failed'", id).RowsAffected(); n != 1 {
		fail(409, "仅明确失败且未创建实例的请求可重试；不确定的记录需先核对钉钉")
	}
	send(w, 200, Row{"ok": true})
}

// Recover an ambiguous creation only by binding a provider instance whose
// immutable participants and document number match this local request.
func (a *App) bindApprovalHTTP(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	ctx, id := r.Context(), routeID(r, "id")
	if num(actor(r)["id"]) != a.Config.FixedApproverID {
		fail(403, "只有固定管理员可核对审批")
	}
	if !a.oaEnabled() {
		fail(503, "钉钉审批未启用")
	}
	instance := text(body(r)["instanceId"], 200, true)
	if !a.workflowMu.TryLock() {
		fail(429, "正在同步审批，请稍后重试")
	}
	defer a.workflowMu.Unlock()
	oa := one(ctx, a.DB, "SELECT * FROM workflow_oa WHERE flow_id=?", id)
	if oa == nil || oa["instance_id"] != nil || oa["state"] != "uncertain" {
		fail(409, "此申请无需核对实例")
	}
	result := a.workflowCall(ctx, "GET", "/v1.0/workflow/processInstances?processInstanceId="+url.QueryEscape(instance), nil)
	detail, _ := result["result"].(map[string]any)
	f := one(ctx, a.DB, "SELECT document_no FROM asset_flows WHERE id=?", id)
	matched := false
	if fields, ok := detail["formComponentValues"].([]any); ok {
		for _, raw := range fields {
			field, _ := raw.(map[string]any)
			if field["name"] == "系统单号" && field["value"] == f["document_no"] {
				matched = true
			}
		}
	}
	approvers, _ := detail["approverUserIds"].([]any)
	if !matched || str(detail["originatorUserId"]) != str(oa["originator_user_id"]) || len(approvers) != 1 || str(approvers[0]) != str(oa["approver_user_id"]) {
		fail(409, "钉钉实例的单号、申请人或指定审批人不匹配")
	}
	transaction(ctx, a.DB, func(tx *sql.Tx) bool {
		exec(ctx, tx, "UPDATE workflow_oa SET instance_id=?,state='active',last_error='' WHERE flow_id=? AND state='uncertain' AND instance_id IS NULL", instance, id)
		audit(ctx, tx, actor(r), "oa_instance_reconciled", str(id), Row{"instanceId": instance})
		return true
	})
	a.syncOneApproval(ctx, id)
	send(w, 200, flowDetail(ctx, a.DB, id))
}

func (a *App) ApprovalWorker(ctx context.Context) {
	if !a.oaEnabled() {
		return
	}
	// The prior process might have stopped after creation but before persisting ID.
	exec(ctx, a.DB, "UPDATE workflow_oa SET state='uncertain',last_error='服务重启前创建结果未确认，请先核对钉钉审批记录' WHERE state='creating'")
	exec(ctx, a.DB, "UPDATE notification_outbox n JOIN notification_deliveries d ON d.notification_id=n.id SET n.status='failed',d.state='uncertain',d.last_error='服务重启前发送结果未确认，不自动重复推送' WHERE d.state='sending'")
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			func() {
				defer func() {
					if recover() != nil {
						slog.Warn("approval worker will retry")
					}
				}()
				for _, oa := range rows(ctx, a.DB, "SELECT flow_id FROM workflow_oa WHERE state IN ('queued','active') AND next_attempt<=CURRENT_TIMESTAMP(3) ORDER BY next_attempt LIMIT 10") {
					func() {
						a.workflowMu.Lock()
						defer a.workflowMu.Unlock()
						work, cancel := context.WithTimeout(ctx, 25*time.Second)
						defer cancel()
						a.syncOneApproval(work, num(oa["flow_id"]))
					}()
				}
				a.deliverFlowNotifications(ctx)
				a.checkFlowNotifications(ctx)
			}()
		}
	}
}

func (a *App) deliverFlowNotifications(ctx context.Context) {
	// Only notifications produced by this rollout's OA-bound flows are eligible.
	// Old queued notifications are historical and must not be sent unexpectedly.
	for _, n := range rows(ctx, a.DB, "SELECT n.* FROM notification_outbox n JOIN workflow_oa o ON o.flow_id=n.flow_id LEFT JOIN notification_deliveries d ON d.notification_id=n.id WHERE n.kind<>'approval_pending' AND n.status='pending' AND (d.state IS NULL OR (d.state='queued' AND d.next_attempt<=CURRENT_TIMESTAMP(3))) ORDER BY n.id LIMIT 10") {
		func() {
			defer func() {
				if p := recover(); p != nil {
					state, message := workflowProblem(p)
					cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					status := "failed"
					if issue, ok := p.(problem); ok && issue.Status == 502 && num(one(cleanup, a.DB, "SELECT attempts FROM notification_deliveries WHERE notification_id=?", n["id"])["attempts"]) < 3 {
						state = "queued"
						status = "pending"
					}
					exec(cleanup, a.DB, "UPDATE notification_deliveries SET state=?,last_error=?,next_attempt=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 60 SECOND) WHERE notification_id=?", state, message, n["id"])
					exec(cleanup, a.DB, "UPDATE notification_outbox SET status=? WHERE id=?", status, n["id"])
				}
			}()
			exec(ctx, a.DB, "INSERT INTO notification_deliveries(notification_id) VALUES (?) ON DUPLICATE KEY UPDATE notification_id=notification_id", n["id"])
			recipient := one(ctx, a.DB, "SELECT e.dingtalk_user_id,e.corp_id FROM users u JOIN employees e ON e.id=u.employee_id WHERE u.id=? AND u.active=1 AND e.active=1 AND e.missing_since IS NULL", n["recipient_id"])
			if recipient == nil || str(recipient["corp_id"]) != a.Config.DingCorp {
				fail(409, "通知接收人的钉钉身份已失效")
			}
			payload := Row{}
			decode(str(n["payload_json"]), &payload)
			original, _ := payload["recipient"].(map[string]any)
			if str(original["userId"]) != str(recipient["dingtalk_user_id"]) || str(original["corpId"]) != a.Config.DingCorp {
				fail(409, "通知接收人身份已变化，停止发送")
			}
			labels := map[string]string{"approved": "申请已批准，等待分配设备", "completed": "申请已完成", "rejected": "申请已驳回", "withdrawn": "申请已撤回"}
			f := one(ctx, a.DB, "SELECT document_no FROM asset_flows WHERE id=?", n["flow_id"])
			exec(ctx, a.DB, "UPDATE notification_deliveries SET state='sending',attempts=attempts+1 WHERE notification_id=?", n["id"])
			response := a.workMessageCall(ctx, "/topapi/message/corpconversation/asyncsend_v2", Row{"agent_id": a.Config.DingAgentID, "userid_list": recipient["dingtalk_user_id"], "msg": Row{"msgtype": "link", "link": Row{"messageUrl": fmt.Sprintf("%s/?flow=%v", a.publicURL(), n["flow_id"]), "picUrl": a.publicURL() + "/brand/jiudun-mascot.png", "title": "九吨资产 · " + labels[str(n["kind"])], "text": str(f["document_no"]) + "，点击查看申请详情"}}})
			if str(response["task_id"]) == "" {
				fail(504, "通知发送结果无法确认")
			}
			exec(ctx, a.DB, "UPDATE notification_deliveries SET state='accepted',task_id=?,last_error='',next_attempt=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 15 SECOND) WHERE notification_id=?", str(response["task_id"]), n["id"])
			// The provider accepted a task, not yet a confirmed device delivery.
			exec(ctx, a.DB, "UPDATE notification_outbox SET status='accepted',sent_at=CURRENT_TIMESTAMP(3) WHERE id=?", n["id"])
		}()
	}
}

func (a *App) workMessageCall(ctx context.Context, path string, payload Row) Row {
	base := strings.TrimRight(a.Config.DingOAPI, "/")
	if base == "" {
		base = "https://oapi.dingtalk.com"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", base+path+"?access_token="+url.QueryEscape(a.dingToken(ctx)), bytes.NewBufferString(js(payload)))
	must(err)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		fail(504, "钉钉通知发送结果不确定，不会重复推送")
	}
	defer res.Body.Close()
	var data Row
	d := json.NewDecoder(io.LimitReader(res.Body, 2<<20))
	d.UseNumber()
	if d.Decode(&data) != nil || data == nil || res.StatusCode >= 500 {
		fail(504, "钉钉通知响应无法确认")
	}
	if res.StatusCode >= 300 || num(data["errcode"]) != 0 {
		if num(data["errcode"]) == 40014 || num(data["errcode"]) == 42001 {
			a.tokenMu.Lock()
			a.token = ""
			a.tokenUntil = time.Time{}
			a.tokenMu.Unlock()
		}
		fail(502, fmt.Sprintf("钉钉工作通知接口拒绝请求（%d）", num(data["errcode"])))
	}
	return data
}

func (a *App) checkFlowNotifications(ctx context.Context) {
	for _, n := range rows(ctx, a.DB, "SELECT n.id,n.payload_json,d.task_id FROM notification_outbox n JOIN notification_deliveries d ON d.notification_id=n.id WHERE d.state='accepted' AND d.next_attempt<=CURRENT_TIMESTAMP(3) ORDER BY n.id LIMIT 10") {
		func() {
			defer func() {
				if recover() != nil {
					exec(ctx, a.DB, "UPDATE notification_deliveries SET next_attempt=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 60 SECOND) WHERE notification_id=?", n["id"])
				}
			}()
			response := a.workMessageCall(ctx, "/topapi/message/corpconversation/getsendresult", Row{"agent_id": a.Config.DingAgentID, "task_id": num(n["task_id"])})
			result, _ := response["send_result"].(map[string]any)
			payload := Row{}
			decode(str(n["payload_json"]), &payload)
			recipient, _ := payload["recipient"].(map[string]any)
			state, status, message := "accepted", "accepted", ""
			for _, key := range []string{"read_user_id_list", "unread_user_id_list"} {
				if list, ok := result[key].([]any); ok {
					for _, id := range list {
						if str(id) == str(recipient["userId"]) {
							state = "sent"
							status = "sent"
						}
					}
				}
			}
			for _, key := range []string{"failed_user_id_list", "invalid_user_id_list", "forbidden_list"} {
				if list, ok := result[key].([]any); ok && len(list) > 0 {
					state = "failed"
					status = "failed"
					message = "钉钉报告通知接收失败，请核对应用可见范围和接收人状态"
				}
			}
			exec(ctx, a.DB, "UPDATE notification_deliveries SET state=?,last_error=?,next_attempt=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 60 SECOND) WHERE notification_id=?", state, message, n["id"])
			exec(ctx, a.DB, "UPDATE notification_outbox SET status=? WHERE id=?", status, n["id"])
		}()
	}
}
