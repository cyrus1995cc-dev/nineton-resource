package app

import (
	"fmt"
	"strings"
	"time"
)

const receiptInstruction = "请收到并核对设备后，点击同意确认领用。"

// Use the immutable flow snapshots: later edits to an asset must not rewrite
// the equipment described by an earlier approval or result notification.
func workflowEquipment(items []Row, limit int) string {
	lines := []string{}
	used := 0
	for _, item := range items {
		line := str(item["asset_name"]) + "（" + str(item["asset_code"]) + "）"
		if model := str(item["model"]); model != "" {
			line += " · " + model
		}
		if used+len([]rune(line))+1 > limit {
			break
		}
		lines = append(lines, line)
		used += len([]rune(line)) + 1
	}
	if remaining := len(items) - len(lines); remaining > 0 {
		lines = append(lines, fmt.Sprintf("另有 %d 件设备，请查看完整单据。", remaining))
	}
	return strings.Join(lines, "\n")
}

func workflowTime(value any) string {
	for _, format := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.Parse(format, str(value)); err == nil {
			return t.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04")
		}
	}
	return "未记录"
}

func (a *App) approvalFormValues(f, demand Row, items []Row, dedicatedDispatch bool) []Row {
	link := fmt.Sprintf("%s/?flow=%v", a.publicURL(), f["id"])
	equipment := workflowEquipment(items, 3000)
	if dedicatedDispatch {
		values := []Row{
			{"name": "设备信息", "value": equipment},
			{"name": "接收员工", "value": f["to_assignee"]},
			{"name": "接收部门", "value": f["to_department"]},
			{"name": "派发人", "value": f["applicant_name"]},
			{"name": "派发部门", "value": f["applicant_department"]},
			{"name": "派发时间", "value": workflowTime(f["created_at"])},
			{"name": "领用说明", "value": receiptInstruction},
		}
		if note := str(f["note"]); note != "" {
			values = append(values, Row{"name": "补充说明", "value": note})
		}
		return append(values, Row{"name": "系统单号", "value": f["document_no"]}, Row{"name": "查看单据", "value": link})
	}
	content := []string{}
	if demand != nil {
		content = append(content, fmt.Sprintf("设备需求：%s · %d 件", str(demand["category_path"]), num(demand["quantity"])), "需求日期："+str(demand["needed_date"]), "用途："+str(demand["purpose"]))
	} else if equipment != "" {
		content = append(content, "设备："+equipment)
	}
	for _, field := range []struct{ key, label string }{{"to_assignee", "接收员工"}, {"to_department", "接收部门"}, {"applicant_name", "发起人"}, {"applicant_department", "发起部门"}, {"to_location", "目标位置"}} {
		if value := str(f[field.key]); value != "" {
			content = append(content, field.label+"："+value)
		}
	}
	if f["type"] == "派发" {
		content = append(content, receiptInstruction)
	}
	if note := str(f["note"]); note != "" && demand == nil {
		content = append(content, "补充说明："+note)
	}
	content = append(content, "查看单据："+link)
	if equipment == "" {
		equipment = "按设备类别申请，批准后由管理员分配具体设备。"
	}
	return []Row{{"name": "系统单号", "value": f["document_no"]}, {"name": "申请类型", "value": f["type"]}, {"name": "申请内容", "value": strings.Join(content, "\n")}, {"name": "资产清单", "value": equipment}}
}

func workflowResultMessage(f, demand Row, items []Row, kind, reason string) Row {
	equipment := workflowEquipment(items, 240)
	labels := map[string]string{"approved": "需求已批准 · 待分配", "completed": "申请已完成", "rejected": "申请已驳回", "withdrawn": "申请已撤回"}
	label := labels[kind]
	message := str(f["applicant_name"]) + "的资产" + str(f["type"])
	if f["type"] == "派发" {
		switch kind {
		case "completed":
			label = "员工已确认领用"
			message = str(f["to_assignee"]) + "已确认领用，资产登记完成。"
		case "rejected":
			label = "员工拒绝领用"
			message = str(f["to_assignee"]) + "已拒绝领用，资产归属未变更。"
		case "withdrawn":
			label = "资产派发已撤回"
			message = str(f["applicant_name"]) + "已撤回向" + str(f["to_assignee"]) + "的派发，资产归属未变更。"
		}
	} else {
		switch kind {
		case "completed":
			message += "已完成，资产台账已更新。"
		case "approved":
			message += "需求已获批准，等待管理员分配设备，尚未登记资产。"
		case "rejected":
			message += "已驳回，资产归属未变更。"
		case "withdrawn":
			message += "已撤回，资产归属未变更。"
		}
	}
	if equipment != "" {
		message += "\n设备：" + equipment
	} else if demand != nil {
		message += fmt.Sprintf("\n设备需求：%s · %d 件", str(demand["category_path"]), num(demand["quantity"]))
	}
	if reason != "" && (kind == "rejected" || kind == "withdrawn") {
		reasonRunes := []rune(reason)
		if len(reasonRunes) > 200 {
			reason = string(reasonRunes[:200]) + "…"
		}
		message += "\n原因：" + reason
	}
	message += "\n单号：" + str(f["document_no"])
	title := "九吨资产 · " + label
	if len(items) == 1 {
		summary := str(items[0]["asset_name"]) + " " + str(items[0]["asset_code"])
		if runes := []rune(summary); len(runes) > 45 {
			summary = string(runes[:45]) + "…"
		}
		title += " · " + summary
	} else if len(items) > 1 {
		title += fmt.Sprintf(" · %d 件设备", len(items))
	}
	if runes := []rune(message); len(runes) > 500 {
		message = string(runes[:480]) + "…\n请打开单据查看完整信息。"
	}
	return Row{"title": title, "text": message}
}
