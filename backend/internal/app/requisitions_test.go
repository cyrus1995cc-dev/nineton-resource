package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func requisitionFixture(t *testing.T) (*App, testClient, testClient, int64) {
	a, admin := testApp(t)
	ctx := context.Background()
	a.Config.FixedApproverID = num(one(ctx, a.DB, "SELECT id FROM users WHERE role='admin'")["id"])
	owner, _ := directorySSOEmployee(t, a, "demand-owner", "员工甲")
	exec(ctx, a.DB, "UPDATE employees SET department='研发部' WHERE id=?", owner)
	u := one(ctx, a.DB, userJoin+"WHERE u.employee_id=?", owner)
	login := a.session(ctx, a.DB, u)
	category := num(one(ctx, a.DB, "SELECT id FROM asset_categories WHERE code='BJB'")["id"])
	for _, code := range []string{"REQ-001", "REQ-002", "REQ-003"} {
		seed(ctx, a.DB, code, "空闲")
		exec(ctx, a.DB, "UPDATE assets SET category_path=(SELECT path FROM asset_categories WHERE id=?) WHERE asset_code=?", category, code)
	}
	return a, admin, testClient{t: t, handler: a.Handler(), token: str(login["token"])}, category
}

func demandInput(category int64, key string) Row {
	return Row{"categoryId": category, "quantity": 1, "neededDate": "2026-12-01", "purpose": "办公使用", "requestId": key}
}

func TestRequisitionOwnershipAllocationAndApproval(t *testing.T) {
	a, admin, employee, category := requisitionFixture(t)
	ctx := context.Background()
	options := employee.call("GET", "/api/requisitions/options", nil, 200)
	if strings.Contains(js(options), "assets_count") || strings.Contains(js(options), "REQ-001") {
		t.Fatal("inventory leaked")
	}
	before := js(rows(ctx, a.DB, "SELECT * FROM assets ORDER BY id"))
	created := employee.call("POST", "/api/requisitions", demandInput(category, "demand-first"), 201)
	id := num(created["flow"].(map[string]any)["id"])
	path := "/api/flows/" + str(id)
	if before != js(rows(ctx, a.DB, "SELECT * FROM assets ORDER BY id")) {
		t.Fatal("request changed assets")
	}
	employee.call("POST", "/api/requisitions", demandInput(category, "demand-first"), 200)
	wrong := demandInput(category, "demand-first")
	wrong["quantity"] = 2
	employee.call("POST", "/api/requisitions", wrong, 409)
	employee.call("GET", "/api/assets/REQ-001", nil, 404)
	employee.call("POST", path+"/allocation", Row{"assetCodes": []string{"REQ-001"}, "expectedVersion": 1}, 403)
	admin.call("POST", path+"/approve", Row{"comment": "同意需求"}, 200)
	if one(ctx, a.DB, "SELECT status FROM asset_flows WHERE id=?", id)["status"] != "approved" {
		t.Fatal("empty demand completed")
	}
	admin.call("POST", path+"/allocation", Row{"assetCodes": []string{"REQ-001"}, "expectedVersion": 1}, 200)
	employee.call("GET", "/api/assets/REQ-001", nil, 200)
	if num(employee.call("GET", "/api/stats", nil, 200)["total"]) != 1 {
		t.Fatal("assignment not visible")
	}
	admin.call("POST", path+"/allocation", Row{"assetCodes": []string{"REQ-001"}, "expectedVersion": 1}, 409)
	second := employee.call("POST", "/api/requisitions", demandInput(category, "demand-second"), 201)
	secondID := second["flow"].(map[string]any)["id"]
	secondPath := "/api/flows/" + str(secondID)
	admin.call("POST", secondPath+"/allocation", Row{"assetCodes": []string{"REQ-002"}, "expectedVersion": 1}, 200)
	third := employee.call("POST", "/api/requisitions", demandInput(category, "demand-third"), 201)
	thirdPath := "/api/flows/" + str(third["flow"].(map[string]any)["id"])
	admin.call("POST", thirdPath+"/allocation", Row{"assetCodes": []string{"REQ-002"}, "expectedVersion": 1}, 409)
	admin.call("POST", secondPath+"/approve", Row{}, 200)
	if num(employee.call("GET", "/api/stats", nil, 200)["total"]) != 2 {
		t.Fatal("allocate-first failed")
	}
	employee.call("POST", thirdPath+"/withdraw", Row{}, 200)
	other, _ := directorySSOEmployee(t, a, "demand-other", "员工乙")
	login := a.session(ctx, a.DB, one(ctx, a.DB, userJoin+"WHERE u.employee_id=?", other))
	outsider := testClient{t: t, handler: a.Handler(), token: str(login["token"])}
	outsider.call("GET", path, nil, 404)
	outsider.call("POST", path+"/oa-sync", Row{}, 404)
	outsider.call("GET", "/api/assets/REQ-001", nil, 404)
}

func TestDingTalkApprovalIsFixedVerifiedAndIdempotent(t *testing.T) {
	a, admin, employee, category := requisitionFixture(t)
	ctx := context.Background()
	owner, _ := directorySSOEmployee(t, a, "fixed-admin", "管理员")
	exec(ctx, a.DB, "UPDATE users SET employee_id=NULL,active=0 WHERE employee_id=?", owner)
	exec(ctx, a.DB, "UPDATE users SET employee_id=? WHERE id=?", owner, a.Config.FixedApproverID)
	a.Config.DingCorp = "test-corp"
	a.Config.DingID = "app"
	a.Config.DingSecret = "server-secret"
	a.Config.DingAgentID = 123
	a.Config.DingProcessCode = "PROC-TEST"
	a.Config.DingApprovalEnabled = true
	createdCount, wrongApprover, decision, simulateTimeout := 0, false, "agree", false
	remoteStatus, remoteDoc := "COMPLETED", ""
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			send(w, 200, Row{"accessToken": "test-token", "expireIn": 7200})
			return
		}
		if r.URL.Path == "/v1.0/workflow/processInstances" && r.Method == "POST" {
			createdCount++
			b := body(r)
			if b["bizDetailPageUrl"] != nil {
				t.Error("paid OA field must not be required")
			}
			if b["originatorUserId"] != "demand-owner" || b["processCode"] != "PROC-TEST" || !strings.Contains(js(b["approvers"]), "fixed-admin") || strings.Contains(js(b), "server-secret") {
				t.Error("invalid OA identity")
			}
			if simulateTimeout {
				send(w, 503, Row{"code": "timeout"})
				return
			}
			send(w, 200, Row{"instanceId": "native-" + str(createdCount)})
			return
		}
		if r.URL.Path == "/v1.0/workflow/processInstances" && r.Method == "GET" {
			approver := "fixed-admin"
			if wrongApprover {
				approver = "other-admin"
			}
			send(w, 200, Row{"result": Row{"status": remoteStatus, "result": decision, "originatorUserId": "demand-owner", "approverUserIds": []string{"fixed-admin"}, "formComponentValues": []Row{{"name": "系统单号", "value": remoteDoc}}, "operationRecords": []Row{{"userId": approver, "result": strings.ToUpper(decision), "remark": "测试结果"}}}})
			return
		}
		if r.URL.Path == "/v1.0/workflow/processInstances/terminate" {
			send(w, 200, Row{"success": true, "result": true})
			return
		}
		if r.URL.Path == "/topapi/message/corpconversation/asyncsend_v2" {
			if !strings.Contains(js(body(r)), "/brand/jiudun-mascot.png") {
				t.Error("missing message image")
			}
			send(w, 200, Row{"errcode": 0, "task_id": 123})
			return
		}
		if r.URL.Path == "/topapi/message/corpconversation/getsendresult" {
			send(w, 200, Row{"errcode": 0, "send_result": Row{"unread_user_id_list": []string{"demand-owner"}}})
			return
		}
		t.Error("unexpected endpoint", r.URL.Path)
		send(w, 404, Row{})
	}))
	defer mock.Close()
	a.Config.DingAPI = mock.URL
	a.Config.DingOAPI = mock.URL
	created := employee.call("POST", "/api/requisitions", demandInput(category, "native-demand"), 201)
	id := num(created["flow"].(map[string]any)["id"])
	path := "/api/flows/" + str(id)
	admin.call("POST", path+"/approve", Row{}, 409)
	wrongApprover = true
	a.syncOneApproval(ctx, id)
	if one(ctx, a.DB, "SELECT status FROM asset_flows WHERE id=?", id)["status"] != "pending" {
		t.Fatal("other approver accepted")
	}
	wrongApprover = false
	a.syncOneApproval(ctx, id)
	a.syncOneApproval(ctx, id)
	if createdCount != 1 || one(ctx, a.DB, "SELECT status FROM asset_flows WHERE id=?", id)["status"] != "approved" {
		t.Fatal("result or duplicate", createdCount)
	}
	admin.call("POST", path+"/allocation", Row{"assetCodes": []string{"REQ-001"}, "expectedVersion": 1}, 200)
	a.syncOneApproval(ctx, id)
	if num(one(ctx, a.DB, "SELECT COUNT(*) n FROM asset_events WHERE asset_code='REQ-001' AND type='领用'")["n"]) != 1 {
		t.Fatal("repeated approval wrote twice")
	}
	a.deliverFlowNotifications(ctx)
	if num(one(ctx, a.DB, "SELECT COUNT(*) n FROM notification_deliveries WHERE state='accepted'")["n"]) != 1 {
		t.Fatal("result notifications not queued")
	}
	exec(ctx, a.DB, "UPDATE notification_deliveries SET next_attempt=CURRENT_TIMESTAMP(3)")
	a.checkFlowNotifications(ctx)
	if num(one(ctx, a.DB, "SELECT COUNT(*) n FROM notification_deliveries WHERE state='sent'")["n"]) != 1 {
		t.Fatal("delivery not confirmed")
	}
	// A transport timeout must never blindly create a second OA instance.
	failed := employee.call("POST", "/api/requisitions", demandInput(category, "native-timeout"), 201)
	failedID := num(failed["flow"].(map[string]any)["id"])
	simulateTimeout = true
	a.syncOneApproval(ctx, failedID)
	a.syncOneApproval(ctx, failedID)
	if createdCount != 2 || one(ctx, a.DB, "SELECT state FROM workflow_oa WHERE flow_id=?", failedID)["state"] != "uncertain" {
		t.Fatal("uncertain request retried")
	}
	admin.call("POST", "/api/flows/"+str(failedID)+"/oa-retry", Row{}, 409)
	admin.call("POST", "/api/flows/"+str(failedID)+"/oa-bind", Row{"instanceId": "recovered-native"}, 409)
	remoteDoc = str(failed["flow"].(map[string]any)["document_no"])
	admin.call("POST", "/api/flows/"+str(failedID)+"/oa-bind", Row{"instanceId": "recovered-native"}, 200)
	if createdCount != 2 || one(ctx, a.DB, "SELECT status FROM asset_flows WHERE id=?", failedID)["status"] != "approved" {
		t.Fatal("ambiguous instance not reconciled")
	}
	// Rejection and remote withdrawal release the request without changing stock.
	simulateTimeout = false
	decision = "refuse"
	rejected := employee.call("POST", "/api/requisitions", demandInput(category, "native-reject"), 201)
	rejectedID := num(rejected["flow"].(map[string]any)["id"])
	a.syncOneApproval(ctx, rejectedID)
	if one(ctx, a.DB, "SELECT status FROM asset_flows WHERE id=?", rejectedID)["status"] != "rejected" {
		t.Fatal("rejection ignored")
	}
	remoteStatus = "TERMINATED"
	withdrawn := employee.call("POST", "/api/requisitions", demandInput(category, "native-terminate"), 201)
	withdrawnID := num(withdrawn["flow"].(map[string]any)["id"])
	a.syncOneApproval(ctx, withdrawnID)
	if one(ctx, a.DB, "SELECT status FROM asset_flows WHERE id=?", withdrawnID)["status"] != "withdrawn" {
		t.Fatal("remote withdrawal ignored")
	}
	remoteStatus = "RUNNING"
	raced := employee.call("POST", "/api/requisitions", demandInput(category, "native-race"), 201)
	racedID := num(raced["flow"].(map[string]any)["id"])
	a.syncOneApproval(ctx, racedID)
	employee.call("POST", "/api/flows/"+str(racedID)+"/withdraw", Row{}, 200)
	remoteStatus = "COMPLETED"
	decision = "agree"
	a.syncOneApproval(ctx, racedID)
	if one(ctx, a.DB, "SELECT state FROM workflow_oa WHERE flow_id=?", racedID)["state"] != "done" || one(ctx, a.DB, "SELECT status FROM asset_flows WHERE id=?", racedID)["status"] != "withdrawn" {
		t.Fatal("withdrawal race changed local result")
	}
}

func TestRequisitionConcurrentAllocationAndValidation(t *testing.T) {
	a, admin, employee, category := requisitionFixture(t)
	for _, qty := range []any{0, 21, 1.5} {
		input := demandInput(category, "bad-qty")
		input["quantity"] = qty
		employee.call("POST", "/api/requisitions", input, 400)
	}
	input := demandInput(category, "bad-day")
	input["neededDate"] = "2026-02-30"
	employee.call("POST", "/api/requisitions", input, 400)
	ctx := context.Background()
	first := employee.call("POST", "/api/requisitions", demandInput(category, "concurrent-first"), 201)
	second := employee.call("POST", "/api/requisitions", demandInput(category, "concurrent-second"), 201)
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, f := range []Row{first, second} {
		id := f["flow"].(map[string]any)["id"]
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "/api/flows/"+str(id)+"/allocation", strings.NewReader(js(Row{"assetCodes": []string{"REQ-001"}, "expectedVersion": 1})))
			r.Header.Set("Authorization", "Bearer "+admin.token)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			statuses <- w.Code
		}()
	}
	wg.Wait()
	close(statuses)
	wins, conflicts := 0, 0
	for status := range statuses {
		if status == 200 {
			wins++
		} else if status == 409 {
			conflicts++
		} else {
			t.Fatal("unexpected allocation status", status)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("stock allocated twice", wins, conflicts)
	}
	owner := one(ctx, a.DB, "SELECT flow_id FROM asset_flow_items WHERE asset_code='REQ-001'")["flow_id"]
	employee.call("POST", "/api/flows/"+str(owner)+"/withdraw", Row{}, 200)
	if one(ctx, a.DB, "SELECT status FROM assets WHERE asset_code='REQ-001'")["status"] != "空闲" {
		t.Fatal("withdrawal changed stock")
	}
}

func TestFixedAdminDirectHandlingAndEmployeeReturn(t *testing.T) {
	a, admin, employee, _ := requisitionFixture(t)
	ctx := context.Background()
	staff := one(ctx, a.DB, "SELECT id FROM employees WHERE dingtalk_user_id='demand-owner'")
	issued := admin.call("POST", "/api/flows", Row{"type": "领用", "assetCodes": []string{"REQ-001"}, "applicantDepartment": "行政部", "approverIds": []int{999999}, "toEmployeeId": staff["id"], "requestId": "admin-direct-issue"}, 201)
	f := issued["flow"].(map[string]any)
	if f["status"] != "completed" || num(one(ctx, a.DB, "SELECT approver_id FROM asset_flow_steps WHERE flow_id=?", f["id"])["approver_id"]) != a.Config.FixedApproverID {
		t.Fatal("fixed admin not enforced")
	}
	returned := employee.call("POST", "/api/flows", Row{"type": "归还", "assetCodes": []string{"REQ-001"}, "approverIds": []int{999999}, "requestId": "employee-fixed-return"}, 201)
	returnID := returned["flow"].(map[string]any)["id"]
	if returned["flow"].(map[string]any)["status"] != "pending" {
		t.Fatal("employee bypassed approval")
	}
	admin.call("POST", "/api/flows/"+str(returnID)+"/approve", Row{}, 200)
	if one(ctx, a.DB, "SELECT assignee_employee_id FROM assets WHERE asset_code='REQ-001'")["assignee_employee_id"] != nil {
		t.Fatal("return not applied")
	}
}
