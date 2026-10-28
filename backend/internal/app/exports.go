package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

const exportLimit = 50000

var flowLabels = map[string]string{"pending": "审批中", "completed": "已完结", "rejected": "已驳回", "withdrawn": "已撤回"}

func beijing(v any) string {
	s := str(v)
	if s == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02"} {
		if t, e := time.Parse(layout, s); e == nil {
			return t.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
		}
	}
	return s
}
func appendSheet(book *excelize.File, name string, data []Row, cols []column) {
	if len(data) > exportLimit {
		fail(400, "单次最多导出 50000 行，请缩小筛选范围后重试")
	}
	_, e := book.NewSheet(name)
	must(e)
	headers := make([]any, len(cols))
	for i, c := range cols {
		headers[i] = c.Title
	}
	must(book.SetSheetRow(name, "A1", &headers))
	for i, r := range data {
		v := make([]any, len(cols))
		for j, c := range cols {
			v[j] = r[c.Field]
			if v[j] == nil {
				v[j] = ""
			}
		}
		cell, e := excelize.CoordinatesToCellName(1, i+2)
		must(e)
		must(book.SetSheetRow(name, cell, &v))
	}
	last, e := excelize.ColumnNumberToName(len(cols))
	must(e)
	must(book.SetColWidth(name, "A", last, 22))
	must(book.SetRowHeight(name, 1, 24))
	must(book.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2"}))
	if len(data) > 0 {
		must(book.AutoFilter(name, fmt.Sprintf("A1:%s%d", last, len(data)+1), nil))
	}
	style, e := book.NewStyle(&excelize.Style{NumFmt: 4})
	must(e)
	for i, c := range cols {
		if contains(amountFields, c.Field) {
			letter, _ := excelize.ColumnNumberToName(i + 1)
			must(book.SetColStyle(name, letter, style))
		}
	}
}
func (a *App) export(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if r.PathValue("id") != "" {
		kind = "inventory"
	}
	if !contains([]string{"assets", "flows", "inventory"}, kind) {
		fail(404, "导出类型不存在")
	}
	if kind == "inventory" {
		roles(r, "admin", "asset_manager", "viewer")
	}
	a.heavyWork(func() {
		ctx := r.Context()
		book := excelize.NewFile()
		defer book.Close()
		now := beijing(time.Now().UTC().Format(time.RFC3339))
		filename := ""
		counts := Row{}
		filters := r.URL.Query()
		switch kind {
		case "assets":
			where, args := assetWhere(actor(r), filters)
			data := rows(ctx, a.DB, "SELECT * FROM assets"+where+assetOrder(filters)+" LIMIT 50001", args...)
			cols := append(append([]column{}, assetColumns...), column{"qr_raw", "二维码原文"})
			appendSheet(book, "资产清单", data, cols)
			counts["assets"] = len(data)
			filename = "资产清单"
		case "flows":
			where, args := flowWhere(actor(r), filters)
			data := rows(ctx, a.DB, "SELECT f.*,(SELECT COUNT(*) FROM asset_flow_items WHERE flow_id=f.id) AS asset_count,(SELECT approver_name FROM asset_flow_steps WHERE flow_id=f.id AND status='pending' LIMIT 1) AS approver FROM asset_flows f"+where+" ORDER BY f.id DESC LIMIT 50001", args...)
			for _, v := range data {
				v["status_label"] = flowLabels[str(v["status"])]
				v["submitted"] = beijing(v["created_at"])
				v["ended"] = beijing(v["completed_at"])
			}
			appendSheet(book, "流转单据", data, []column{{"document_no", "单据编号"}, {"type", "流转类型"}, {"status_label", "审批状态"}, {"applicant_name", "申请人"}, {"applicant_department", "申请部门"}, {"asset_count", "资产数量"}, {"approver", "当前审批人"}, {"to_assignee", "目标使用人"}, {"to_department", "目标部门"}, {"to_location", "目标位置"}, {"note", "申请说明"}, {"submitted", "申请时间（北京时间）"}, {"ended", "结束时间（北京时间）"}})
			counts["flows"] = len(data)
			for _, spec := range []struct {
				table, name string
				cols        []column
			}{{"asset_flow_items", "流转资产明细", []column{{"document_no", "单据编号"}, {"asset_code", "资产编码"}, {"asset_name", "资产名称"}, {"category_path", "资产分类"}, {"model", "型号"}, {"expected_status", "申请时状态"}, {"expected_assignee", "申请时使用人"}, {"expected_department", "申请时部门"}, {"expected_location", "申请时位置"}}}, {"asset_flow_steps", "审批步骤", []column{{"document_no", "单据编号"}, {"sequence", "审批顺序"}, {"approver_name", "指定审批人"}, {"status_label", "处理状态"}, {"comment", "审批意见"}, {"acted", "处理时间（北京时间）"}}}, {"asset_flow_logs", "处理记录", []column{{"document_no", "单据编号"}, {"action_label", "操作"}, {"actor_name", "处理人"}, {"note", "处理说明"}, {"acted", "操作时间（北京时间）"}}}} {
				related := rows(ctx, a.DB, "SELECT r.*,f.document_no FROM "+spec.table+" r JOIN asset_flows f ON f.id=r.flow_id"+where+" ORDER BY f.id DESC,r.id LIMIT 50001", args...)
				for _, v := range related {
					v["acted"] = beijing(v["acted_at"])
					if spec.table == "asset_flow_logs" {
						v["acted"] = beijing(v["created_at"])
					}
					v["status_label"] = map[string]string{"waiting": "等待前序", "pending": "待审批", "approved": "已同意", "rejected": "已驳回", "cancelled": "已取消"}[str(v["status"])]
					v["action_label"] = map[string]string{"submitted": "提交申请", "approved": "审批通过", "rejected": "驳回申请", "withdrawn": "撤回申请", "completed": "流程完结"}[str(v["action"])]
				}
				appendSheet(book, spec.name, related, spec.cols)
				counts[spec.table] = len(related)
			}
			filename = "流转记录"
		case "inventory":
			id := routeID(r, "id")
			detail := inventoryDetail(ctx, a.DB, id)
			task := detail["task"].(Row)
			data := detail["records"].([]Row)
			task["created"] = beijing(task["created_at"])
			task["completed"] = beijing(task["completed_at"])
			task["status_label"] = "进行中"
			if task["status"] == "completed" {
				task["status_label"] = "已结束"
			}
			for _, state := range []string{"pending", "found", "different", "missing"} {
				task[state] = 0
				for _, v := range data {
					if v["result"] == state {
						task[state] = num(task[state]) + 1
					}
				}
			}
			appendSheet(book, "任务概况", []Row{task}, []column{{"name", "任务名称"}, {"status_label", "任务状态"}, {"location_filter", "位置范围"}, {"department_filter", "部门范围"}, {"operator_name", "创建人"}, {"total_count", "应盘数量"}, {"checked_count", "实际已盘"}, {"pending", "待盘数量"}, {"found", "正常数量"}, {"different", "位置差异"}, {"missing", "未盘到"}, {"difference_count", "差异总数"}, {"created", "创建时间（北京时间）"}, {"completed", "结束时间（北京时间）"}})
			diff := []Row{}
			for _, v := range data {
				v["result_label"] = map[string]string{"pending": "待盘", "found": "正常", "different": "位置差异", "missing": "未盘到"}[str(v["result"])]
				v["checked"] = beijing(v["checked_at"])
				if v["result"] == "different" || v["result"] == "missing" {
					diff = append(diff, v)
				}
			}
			cols := []column{{"asset_code", "资产编码"}, {"asset_name", "资产名称"}, {"expected_status", "任务创建时状态"}, {"expected_assignee", "任务创建时使用人"}, {"expected_department", "任务创建时部门"}, {"expected_location", "任务创建时位置"}, {"result_label", "盘点结果"}, {"actual_location", "实盘位置"}, {"operator_name", "登记/结束人"}, {"checked", "实盘时间（北京时间）"}, {"note", "盘点备注"}}
			appendSheet(book, "全部明细", data, cols)
			appendSheet(book, "差异清单", diff, cols)
			counts["records"] = len(data)
			counts["differences"] = len(diff)
			filename = fmt.Sprintf("盘点结果-%d", id)
		}
		info := []Row{{"key": "导出人", "value": actor(r)["display_name"]}, {"key": "导出时间（北京时间）", "value": now}, {"key": "记录数", "value": js(counts)}, {"key": "筛选条件", "value": filters.Encode()}, {"key": "说明", "value": "按账号权限导出全部筛选结果；归属变更须审批，盘点差异不自动修改台账。"}}
		appendSheet(book, "导出说明", info, []column{{"key", "项目"}, {"value", "内容"}})
		must(book.DeleteSheet("Sheet1"))
		buffer, e := book.WriteToBuffer()
		must(e)
		audit(ctx, a.DB, actor(r), "export", filename, Row{"counts": counts, "filters": filters, "exportedAt": now})
		filename += "-" + strings.NewReplacer("-", "", ":", "", " ", "-").Replace(now) + ".xlsx"
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", "attachment; filename=\"export.xlsx\"; filename*=UTF-8''"+url.PathEscape(filename))
		w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")
		w.WriteHeader(200)
		_, e = w.Write(buffer.Bytes())
		must(e)
	})
}
