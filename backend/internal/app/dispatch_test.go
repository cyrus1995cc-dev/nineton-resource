package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type dispatchRemote struct {
	status, decision, confirmer string
	created, terminated         int
}

func dispatchFixture(t *testing.T) (*App, testClient, testClient, *dispatchRemote) {
	a, admin, employee, _ := requisitionFixture(t)
	ctx := context.Background()
	owner, _ := directorySSOEmployee(t, a, "dispatch-admin", "管理员")
	exec(ctx, a.DB, "UPDATE users SET employee_id=NULL,active=0 WHERE employee_id=?", owner)
	exec(ctx, a.DB, "UPDATE users SET employee_id=? WHERE id=?", owner, a.Config.FixedApproverID)
	a.Config.DingCorp, a.Config.DingID, a.Config.DingSecret = "test-corp", "app", "secret"
	a.Config.DingAgentID, a.Config.DingProcessCode, a.Config.DingApprovalEnabled = 123, "PROC-DISPATCH", true
	a.Config.DingDispatchProcessCode = "PROC-RECEIPT"
	remote := &dispatchRemote{status: "RUNNING", decision: "agree", confirmer: "demand-owner"}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1.0/oauth2/accessToken":
			send(w, 200, Row{"accessToken": "test-token", "expireIn": 7200})
		case r.URL.Path == "/v1.0/workflow/processInstances" && r.Method == "POST":
			b := body(r)
			if b["originatorUserId"] != "dispatch-admin" || js(b["approvers"]) != `[{"actionType":"NONE","userIds":["demand-owner"]}]` {
				t.Error("dispatch must be originated by admin and confirmed only by recipient", js(b["approvers"]))
			}
			fields, _ := b["formComponentValues"].([]any)
			if b["processCode"] != "PROC-RECEIPT" || !strings.Contains(js(fields), receiptInstruction) || !strings.HasPrefix(js(fields), `[{"name":"设备信息"`) {
				t.Error("dispatch must use dedicated receipt form and equipment-first presentation")
			}
			remote.created++
			send(w, 200, Row{"instanceId": "dispatch-" + str(remote.created)})
		case r.URL.Path == "/v1.0/workflow/processInstances" && r.Method == "GET":
			send(w, 200, Row{"result": Row{"status": remote.status, "result": remote.decision, "originatorUserId": "dispatch-admin", "operationRecords": []Row{{"userId": remote.confirmer, "result": strings.ToUpper(remote.decision), "remark": "员工核对实物"}}}})
		case r.URL.Path == "/v1.0/workflow/processInstances/terminate":
			remote.terminated++
			send(w, 200, Row{"success": true, "result": true})
		case r.URL.Path == "/topapi/message/corpconversation/asyncsend_v2":
			b := body(r)
			if b["userid_list"] != "dispatch-admin" && b["userid_list"] != "demand-owner" {
				t.Error("notification sent to unrelated employee")
			}
			if !strings.Contains(js(b["msg"]), "REQ-001") || !strings.Contains(js(b["msg"]), "已确认领用") {
				t.Error("notification must identify the equipment and confirmation result")
			}
			send(w, 200, Row{"errcode": 0, "task_id": 123})
		case r.URL.Path == "/topapi/message/corpconversation/getsendresult":
			send(w, 200, Row{"errcode": 0, "send_result": Row{"unread_user_id_list": []string{"dispatch-admin", "demand-owner"}}})
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			send(w, 404, Row{})
		}
	}))
	t.Cleanup(mock.Close)
	a.Config.DingAPI, a.Config.DingOAPI = mock.URL, mock.URL
	return a, admin, employee, remote
}

func dispatchInput(a *App, kind, code, key string) Row {
	staff := one(context.Background(), a.DB, "SELECT id FROM employees WHERE dingtalk_user_id='demand-owner'")
	return Row{"type": kind, "assetCodes": []string{code}, "applicantDepartment": "行政部", "toEmployeeId": staff["id"], "approverIds": []int{999999}, "requestId": key}
}

func TestDispatchRequiresRecipientConfirmation(t *testing.T) {
	a, admin, employee, remote := dispatchFixture(t)
	ctx := context.Background()
	before := js(rows(ctx, a.DB, "SELECT * FROM assets ORDER BY id"))
	input := dispatchInput(a, "领用", "REQ-001", "legacy-admin-dispatch")
	f := admin.call("POST", "/api/flows", input, 201)
	id := num(f["flow"].(map[string]any)["id"])
	path := "/api/flows/" + str(id)
	if f["flow"].(map[string]any)["type"] != "派发" || f["flow"].(map[string]any)["status"] != "pending" || f["oa"] == nil {
		t.Fatal("administrator assignment bypassed recipient confirmation")
	}
	if before != js(rows(ctx, a.DB, "SELECT * FROM assets ORDER BY id")) || num(employee.call("GET", "/api/stats", nil, 200)["total"]) != 0 {
		t.Fatal("dispatch changed ownership before confirmation")
	}
	admin.call("POST", "/api/flows", input, 200)
	input["type"], input["approverIds"] = "派发", []int{1}
	admin.call("POST", "/api/flows", input, 200)
	admin.call("POST", "/api/flows", dispatchInput(a, "派发", "REQ-001", "duplicate-reservation"), 409)
	employee.call("GET", path, nil, 200)
	todo := employee.call("GET", "/api/flows?scope=todo", nil, 200)
	if num(todo["total"]) != 1 {
		t.Fatal("recipient confirmation missing from their pending tasks")
	}
	employee.call("GET", "/api/assets/REQ-001", nil, 404)
	employee.call("POST", "/api/flows", dispatchInput(a, "派发", "REQ-002", "employee-cannot-dispatch"), 403)
	admin.call("POST", path+"/approve", Row{}, 409)
	admin.call("POST", path+"/reject", Row{"comment": "代确认"}, 409)
	a.syncOneApproval(ctx, id)
	remote.status, remote.confirmer = "COMPLETED", "dispatch-admin"
	a.syncOneApproval(ctx, id)
	if one(ctx, a.DB, "SELECT status FROM asset_flows WHERE id=?", id)["status"] != "pending" {
		t.Fatal("administrator confirmed on recipient's behalf")
	}
	remote.confirmer = "demand-owner"
	a.syncOneApproval(ctx, id)
	a.syncOneApproval(ctx, id)
	if remote.created != 1 || one(ctx, a.DB, "SELECT status FROM asset_flows WHERE id=?", id)["status"] != "completed" {
		t.Fatal("recipient confirmation failed or duplicate OA created")
	}
	if num(employee.call("GET", "/api/stats", nil, 200)["total"]) != 1 || num(one(ctx, a.DB, "SELECT COUNT(*) n FROM asset_events WHERE asset_code='REQ-001' AND type='派发'")["n"]) != 1 {
		t.Fatal("confirmed asset must appear exactly once under recipient")
	}
	if num(one(ctx, a.DB, "SELECT COUNT(*) n FROM notification_outbox WHERE flow_id=? AND kind='completed'", id)["n"]) != 2 {
		t.Fatal("both dispatcher and recipient need completion notification")
	}
	a.deliverFlowNotifications(ctx)
	exec(ctx, a.DB, "UPDATE notification_deliveries SET next_attempt=CURRENT_TIMESTAMP(3)")
	a.checkFlowNotifications(ctx)
	if num(one(ctx, a.DB, "SELECT COUNT(*) n FROM notification_outbox WHERE flow_id=? AND kind='completed' AND status='sent'", id)["n"]) != 2 {
		t.Fatal("dispatch result notifications were not delivered to both participants")
	}
	other, _ := directorySSOEmployee(t, a, "other-recipient", "其他员工")
	login := a.session(ctx, a.DB, one(ctx, a.DB, userJoin+"WHERE u.employee_id=?", other))
	outsider := testClient{t: t, handler: a.Handler(), token: str(login["token"])}
	outsider.call("GET", path, nil, 404)
	outsider.call("POST", path+"/oa-sync", Row{}, 404)
	outsider.call("GET", "/api/assets/REQ-001", nil, 404)
}

func TestDispatchRefusalWithdrawalAndIdentityGuards(t *testing.T) {
	a, admin, _, remote := dispatchFixture(t)
	ctx := context.Background()
	remote.status, remote.decision = "COMPLETED", "refuse"
	rejected := admin.call("POST", "/api/flows", dispatchInput(a, "派发", "REQ-001", "recipient-refuses"), 201)
	rejectedID := num(rejected["flow"].(map[string]any)["id"])
	a.syncOneApproval(ctx, rejectedID)
	if one(ctx, a.DB, "SELECT status FROM asset_flows WHERE id=?", rejectedID)["status"] != "rejected" || one(ctx, a.DB, "SELECT status FROM assets WHERE asset_code='REQ-001'")["status"] != "空闲" {
		t.Fatal("refused dispatch mutated stock")
	}
	remote.status, remote.decision = "RUNNING", "agree"
	withdrawn := admin.call("POST", "/api/flows", dispatchInput(a, "派发", "REQ-001", "admin-withdraws"), 201)
	id := num(withdrawn["flow"].(map[string]any)["id"])
	a.syncOneApproval(ctx, id)
	admin.call("POST", "/api/flows/"+str(id)+"/withdraw", Row{}, 200)
	a.syncOneApproval(ctx, id)
	if remote.terminated != 1 {
		t.Fatal("withdrawn dispatch did not terminate native approval")
	}
	remote.status = "COMPLETED"
	a.syncOneApproval(ctx, id)
	if one(ctx, a.DB, "SELECT status FROM assets WHERE asset_code='REQ-001'")["status"] != "空闲" {
		t.Fatal("late confirmation changed withdrawn dispatch")
	}
	changed := admin.call("POST", "/api/flows", dispatchInput(a, "派发", "REQ-002", "identity-changed"), 201)
	changedID := num(changed["flow"].(map[string]any)["id"])
	exec(ctx, a.DB, "UPDATE employees SET dingtalk_user_id='reassigned-user' WHERE dingtalk_user_id='demand-owner'")
	a.syncOneApproval(ctx, changedID)
	if one(ctx, a.DB, "SELECT status FROM asset_flows WHERE id=?", changedID)["status"] != "pending" || one(ctx, a.DB, "SELECT status FROM assets WHERE asset_code='REQ-002'")["status"] != "空闲" {
		t.Fatal("identity change bypassed confirmation guard")
	}
	exec(ctx, a.DB, "UPDATE employees SET dingtalk_user_id='demand-owner' WHERE dingtalk_user_id='reassigned-user'")
	a.Config.DingApprovalEnabled = false
	admin.call("POST", "/api/flows", dispatchInput(a, "派发", "REQ-003", "oa-disabled"), 503)
	a.Config.DingApprovalEnabled = true
	self := dispatchInput(a, "派发", "REQ-003", "self-dispatch")
	self["toEmployeeId"] = one(ctx, a.DB, "SELECT employee_id FROM users WHERE id=?", a.Config.FixedApproverID)["employee_id"]
	admin.call("POST", "/api/flows", self, 400)
}
