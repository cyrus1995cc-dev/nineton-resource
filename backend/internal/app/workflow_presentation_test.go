package app

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func presentationFixture() (*App, Row, []Row) {
	a := &App{Config: Config{PublicURL: "https://assets.example"}}
	f := Row{"id": 3, "type": "派发", "document_no": "ZCPF2026110300003", "applicant_name": "张洋华", "applicant_department": "行政部", "to_assignee": "接收员工", "to_department": "研发部", "created_at": "2026-11-03 09:18:00", "note": "配套电源已附"}
	items := []Row{{"asset_name": "显示器", "asset_code": "XSQ-0243", "model": "27英寸"}}
	return a, f, items
}

func TestDispatchFormMatchesPublishedTemplate(t *testing.T) {
	a, f, items := presentationFixture()
	values := a.approvalFormValues(f, nil, items, true)
	if values[0]["name"] != "设备信息" || values[1]["name"] != "接收员工" || values[2]["name"] != "接收部门" || !strings.Contains(str(values[0]["value"]), "XSQ-0243") {
		t.Fatal("equipment and recipient must lead the native approval")
	}
	encoded, err := os.ReadFile("../../../deploy/dingtalk-dispatch-form.json")
	if err != nil {
		t.Fatal(err)
	}
	var template struct {
		Name   string `json:"name"`
		Fields []struct {
			Props struct {
				Label    string `json:"label"`
				Required bool   `json:"required"`
			} `json:"props"`
		} `json:"formComponents"`
	}
	if err := json.Unmarshal(encoded, &template); err != nil {
		t.Fatal(err)
	}
	if template.Name != "九吨资产·领用确认" {
		t.Fatal("wrong template title")
	}
	fields := map[string]string{}
	for _, v := range values {
		fields[str(v["name"])] = str(v["value"])
	}
	for _, field := range template.Fields {
		if field.Props.Required && fields[field.Props.Label] == "" {
			t.Fatal("missing required native field", field.Props.Label)
		}
		delete(fields, field.Props.Label)
	}
	if len(fields) != 0 {
		t.Fatal("fields not defined by deployed form", fields)
	}
	if workflowTime(f["created_at"]) != "2026-11-03 17:18" {
		t.Fatal("dispatch time must display in Asia/Shanghai")
	}
	f["note"] = ""
	if strings.Contains(js(a.approvalFormValues(f, nil, items, true)), `"name":"补充说明"`) {
		t.Fatal("empty optional note must not clutter the form")
	}
}

func TestQueuedLegacyApprovalKeepsOriginalFieldContract(t *testing.T) {
	a, f, items := presentationFixture()
	values := a.approvalFormValues(f, nil, items, false)
	if len(values) != 4 || values[0]["name"] != "系统单号" || values[3]["name"] != "资产清单" {
		t.Fatal("old queued instances must keep the old form schema")
	}
	if !strings.HasPrefix(str(values[2]["value"]), "设备：显示器（XSQ-0243）") || !strings.Contains(str(values[2]["value"]), receiptInstruction) {
		t.Fatal("legacy dispatch still needs concise equipment-first content")
	}
	f["type"] = "领用"
	need := Row{"category_path": "办公设备/显示器", "quantity": 2, "needed_date": "2026-12-01", "purpose": "办公使用"}
	content := str(a.approvalFormValues(f, need, nil, false)[2]["value"])
	if !strings.HasPrefix(content, "设备需求：办公设备/显示器 · 2 件") || strings.Contains(content, receiptInstruction) {
		t.Fatal("staff requests must retain administrator approval language")
	}
}

func TestWorkflowResultIdentifiesEquipmentAndRealOutcome(t *testing.T) {
	_, f, items := presentationFixture()
	for _, test := range []struct{ kind, phrase string }{{"completed", "已确认领用，资产登记完成"}, {"rejected", "已拒绝领用，资产归属未变更"}, {"withdrawn", "已撤回向接收员工的派发"}} {
		message := workflowResultMessage(f, nil, items, test.kind, "设备不符")
		if !strings.Contains(str(message["title"]), "XSQ-0243") || !strings.Contains(str(message["text"]), test.phrase) || !strings.Contains(str(message["text"]), "显示器（XSQ-0243）") {
			t.Fatal(test.kind, message)
		}
		if test.kind == "rejected" && !strings.Contains(str(message["text"]), "原因：设备不符") {
			t.Fatal("rejection reason omitted")
		}
	}
	f["type"] = "领用"
	message := workflowResultMessage(f, Row{"category_path": "显示器", "quantity": 2}, nil, "approved", "")
	if strings.Contains(str(message["text"]), "登记完成") || !strings.Contains(str(message["text"]), "尚未登记资产") {
		t.Fatal("approved demand must not claim completed assignment")
	}
	bulk := []Row{}
	for i := 0; i < 100; i++ {
		bulk = append(bulk, items[0])
	}
	message = workflowResultMessage(f, nil, bulk, "completed", "")
	if len([]rune(str(message["text"]))) > 500 || !strings.Contains(str(message["title"]), "100 件设备") || !strings.Contains(str(message["text"]), "另有") {
		t.Fatal("bulk result must remain bounded and disclose omitted items")
	}
}
