package app

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shakinm/xlsReader/xls"
	"github.com/xuri/excelize/v2"
)

type column struct{ Field, Title string }

var assetColumns = []column{{"asset_code", "资产编码"}, {"category_level1", "资产分类(一级)"}, {"created_at", "创建时间"}, {"category_path", "资产分类"}, {"name", "资产名称"}, {"status", "资产状态"}, {"location", "所在位置"}, {"company", "所属/承租公司"}, {"model", "型号"}, {"cpu", "CPU"}, {"discrete_gpu", "是否有独立显卡"}, {"memory", "内存"}, {"gpu_model", "显卡型号"}, {"serial_number", "设备序列号"}, {"remark", "备注"}, {"photo_url", "资产照片"}, {"assignee", "使用人"}, {"department", "使用部门"}, {"received_at", "领用日期"}, {"amount_taxed", "购置/租赁金额（含税）"}, {"amount_untaxed", "购置/租赁金额（不含税）"}, {"tax_amount", "税额"}}

func valuePresent(v any) bool { return v != nil && str(v) != "" }

func readWorkbook(file io.ReadSeeker) (book *excelize.File) {
	defer func() {
		if p := recover(); p != nil {
			if v, ok := p.(problem); ok {
				panic(v)
			}
			fail(400, "Excel 解析失败，请检查文件内容")
		}
	}()
	magic := make([]byte, 8)
	_, _ = file.Read(magic)
	_, e := file.Seek(0, io.SeekStart)
	must(e)
	if !bytesEqual(magic, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
		book, e = excelize.OpenReader(file, excelize.Options{UnzipSizeLimit: 128 << 20, UnzipXMLSizeLimit: 8 << 20})
		if e != nil {
			fail(400, "Excel 解析失败，请上传有效的 .xlsx 或 .xls 文件")
		}
		return book
	}
	legacy, e := xls.OpenReader(file)
	if e != nil || legacy.GetNumberSheets() == 0 {
		fail(400, "旧版 Excel 解析失败")
	}
	sheet, e := legacy.GetSheet(0)
	must(e)
	if sheet.GetNumberRows() > 20001 {
		fail(400, "每次请导入 1 至 20000 行数据")
	}
	book = excelize.NewFile()
	headers := []string{}
	for i := 0; i < sheet.GetNumberRows(); i++ {
		row, e := sheet.GetRow(i)
		must(e)
		cells := row.GetCols()
		if len(cells) > 256 {
			fail(400, "Excel 列数超过限制")
		}
		values := make([]any, len(cells))
		for j, cell := range cells {
			s := cell.GetString()
			if i == 0 {
				headers = append(headers, s)
			} else if j < len(headers) && (headers[j] == "创建时间" || headers[j] == "领用日期") {
				if serial, e := strconv.ParseFloat(s, 64); e == nil && serial > 0 {
					if date, e := excelize.ExcelDateToTime(serial, false); e == nil {
						s = date.Format("2006-01-02")
					}
				}
			}
			values[j] = s
		}
		axis, _ := excelize.CoordinatesToCellName(1, i+1)
		must(book.SetSheetRow("Sheet1", axis, &values))
	}
	return book
}
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func parseWorkbook(book *excelize.File) []Row {
	names := book.GetSheetList()
	if len(names) == 0 {
		fail(400, "Excel 没有工作表")
	}
	iterator, e := book.Rows(names[0])
	must(e)
	defer iterator.Close()
	if !iterator.Next() {
		fail(400, "Excel 没有表头")
	}
	headers, e := iterator.Columns()
	must(e)
	indices := map[string]int{}
	for i, h := range headers {
		indices[h] = i
	}
	if _, ok := indices["资产编码"]; !ok {
		fail(400, "缺少资产编码表头")
	}
	result := []Row{}
	sheetRow := 1
	for iterator.Next() {
		sheetRow++
		cells, e := iterator.Columns()
		must(e)
		nonempty := false
		for _, c := range cells {
			if strings.TrimSpace(c) != "" {
				nonempty = true
				break
			}
		}
		if !nonempty {
			continue
		}
		if len(result) >= 20000 {
			fail(400, "每次请导入 1 至 20000 行数据")
		}
		row := Row{"_row": sheetRow}
		for _, col := range assetColumns {
			value := ""
			if i, ok := indices[col.Title]; ok && i < len(cells) {
				value = strings.TrimSpace(cells[i])
				if (col.Field == "created_at" || col.Field == "received_at") && value != "" {
					axis, _ := excelize.CoordinatesToCellName(i+1, sheetRow)
					raw, err := book.GetCellValue(names[0], axis, excelize.Options{RawCellValue: true})
					if err == nil {
						if serial, err := strconv.ParseFloat(raw, 64); err == nil && serial > 0 {
							props, err := book.GetWorkbookProps()
							must(err)
							date1904 := props.Date1904 != nil && *props.Date1904
							date, err := excelize.ExcelDateToTime(serial, date1904)
							if err == nil {
								value = date.Format("2006-01-02T15:04:05.000Z")
							}
						}
					}
				}
			}
			if contains(amountFields, col.Field) {
				if value == "" {
					row[col.Field] = nil
				} else {
					clean := strings.NewReplacer(",", "", "￥", "", "¥", "", " ", "").Replace(value)
					n, e := strconv.ParseFloat(clean, 64)
					if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1e12 {
						fail(400, fmt.Sprintf("Excel 第 %d 行「%s」金额格式无效", sheetRow, col.Title))
					}
					row[col.Field] = n
				}
			} else {
				row[col.Field] = value
			}
		}
		result = append(result, row)
	}
	must(iterator.Error())
	if len(result) == 0 {
		fail(400, "每次请导入 1 至 20000 行数据")
	}
	return result
}

type importPlan struct {
	New, Updated, Unchanged []Row
	Errors                  []Row
	Changes                 []Row
	Versions                Row
}

func planImport(ctx context.Context, q DBTX, data []Row, locked bool) importPlan {
	p := importPlan{New: []Row{}, Updated: []Row{}, Unchanged: []Row{}, Errors: []Row{}, Changes: []Row{}, Versions: Row{}}
	seen := map[string]bool{}
	for i, row := range data {
		code := str(row["asset_code"])
		suffix := ""
		if locked {
			suffix = " FOR UPDATE"
		}
		old := one(ctx, q, "SELECT * FROM assets WHERE asset_code=?"+suffix, code)
		if old != nil {
			p.Versions[code] = old["version"]
		} else {
			p.Versions[code] = nil
		}
		reason := ""
		if code == "" {
			reason = "资产编码为空"
		} else if seen[code] {
			reason = "资产编码重复：" + code
		}
		seen[code] = true
		for _, col := range assetColumns {
			if reason != "" {
				break
			}
			value := row[col.Field]
			if !valuePresent(value) && old != nil {
				continue
			}
			if !valuePresent(value) && col.Field != "asset_code" && col.Field != "name" {
				continue
			}
			reason = fieldProblem(col.Field, value)
		}
		if reason == "" && old != nil {
			for _, f := range ownershipFields {
				if valuePresent(row[f]) && str(row[f]) != str(old[f]) {
					reason = "已有资产的状态、使用人、部门、位置或领用日期发生变化，请通过流转审批调整；本行未导入"
					break
				}
			}
		}
		if reason != "" {
			p.Errors = append(p.Errors, Row{"row": i + 2, "assetCode": code, "reason": reason})
			continue
		}
		if old == nil {
			p.New = append(p.New, row)
			continue
		}
		changes := []Row{}
		for _, col := range assetColumns {
			if valuePresent(row[col.Field]) && str(row[col.Field]) != str(old[col.Field]) {
				changes = append(changes, Row{"field": col.Field, "before": old[col.Field], "after": row[col.Field]})
			}
		}
		if len(changes) == 0 {
			p.Unchanged = append(p.Unchanged, row)
		} else {
			p.Updated = append(p.Updated, row)
			p.Changes = append(p.Changes, Row{"assetCode": code, "fields": changes})
		}
	}
	return p
}
func (a *App) previewImport(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	a.heavyWork(func() {
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			fail(400, "上传失败或文件超过 20MB")
		}
		defer r.MultipartForm.RemoveAll()
		file, header, e := r.FormFile("file")
		if e != nil {
			fail(400, "请选择 Excel 文件")
		}
		defer file.Close()
		if header.Size > 20<<20 {
			fail(400, "文件超过 20MB")
		}
		book := readWorkbook(file)
		defer book.Close()
		data := parseWorkbook(book)
		ctx := r.Context()
		p := planImport(ctx, a.DB, data, false)
		id := randomID()
		expires := time.Now().Add(30 * time.Minute).UnixMilli()
		name := text(header.Filename, 200, true)
		exec(ctx, a.DB, "INSERT INTO import_previews (id,actor_id,file_name,rows_json,versions_json,expires_at) VALUES (?,?,?,?,?,?)", id, actor(r)["id"], name, js(data), js(p.Versions), expires)
		send(w, 200, Row{"batchId": id, "fileName": name, "expiresAt": expires, "preview": Row{"newRows": len(p.New), "updatedRows": len(p.Updated), "unchangedRows": len(p.Unchanged), "invalidRows": len(p.Errors)}, "errors": p.Errors, "changes": p.Changes})
	})
}
func (a *App) confirmImport(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	b := body(r)
	id := text(b["batchId"], 100, true)
	a.heavyWork(func() {
		ctx := r.Context()
		result := transaction(ctx, a.DB, func(tx *sql.Tx) Row {
			preview := one(ctx, tx, "SELECT * FROM import_previews WHERE id=? FOR UPDATE", id)
			if preview == nil || num(preview["actor_id"]) != num(actor(r)["id"]) {
				fail(404, "导入预览不存在或不属于当前账号，请重新上传")
			}
			if str(preview["result_json"]) != "" {
				var out Row
				decode(str(preview["result_json"]), &out)
				return out
			}
			if num(preview["expires_at"]) < time.Now().UnixMilli() {
				fail(409, "导入预览已过期，请重新上传")
			}
			var versions Row
			decode(str(preview["versions_json"]), &versions)
			codes := []string{}
			for code := range versions {
				codes = append(codes, code)
			}
			sort.Strings(codes)
			for _, code := range codes {
				old := one(ctx, tx, "SELECT id,version FROM assets WHERE asset_code=? FOR UPDATE", code)
				if (old == nil) != (versions[code] == nil) || old != nil && num(old["version"]) != num(versions[code]) {
					fail(409, code+" 在预览后已变化，请重新上传核对差异")
				}
			}
			var data []Row
			decode(str(preview["rows_json"]), &data)
			plan := planImport(ctx, tx, data, true)
			result := Row{"totalRows": len(data), "newRows": len(plan.New), "updatedRows": len(plan.Updated), "unchangedRows": len(plan.Unchanged), "invalidRows": len(plan.Errors)}
			for _, row := range append(plan.New, plan.Updated...) {
				old := one(ctx, tx, "SELECT * FROM assets WHERE asset_code=? FOR UPDATE", row["asset_code"])
				if old == nil {
					cols := []string{"asset_code"}
					args := []any{row["asset_code"]}
					marks := []string{"?"}
					for _, col := range assetColumns {
						if col.Field == "asset_code" {
							continue
						}
						v := row[col.Field]
						if !valuePresent(v) {
							if contains(amountFields, col.Field) {
								v = 0
							} else if col.Field == "status" {
								v = "空闲"
							} else {
								v = ""
							}
						}
						cols = append(cols, "`"+col.Field+"`")
						args = append(args, v)
						marks = append(marks, "?")
					}
					id := insert(ctx, tx, "INSERT INTO assets ("+strings.Join(cols, ",")+") VALUES ("+strings.Join(marks, ",")+")", args...)
					old = Row{"id": id, "asset_code": row["asset_code"]}
				} else {
					sets := []string{"version=version+1", "updated_at_system=CURRENT_TIMESTAMP(3)"}
					args := []any{}
					for _, col := range assetColumns {
						if col.Field != "asset_code" && valuePresent(row[col.Field]) {
							sets = append(sets, "`"+col.Field+"`=?")
							args = append(args, row[col.Field])
						}
					}
					args = append(args, old["id"])
					exec(ctx, tx, "UPDATE assets SET "+strings.Join(sets, ",")+" WHERE id=?", args...)
				}
				exec(ctx, tx, "UPDATE assets SET source_updated_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), old["id"])
				stored := one(ctx, tx, "SELECT * FROM assets WHERE id=?", old["id"])
				changes := []Row{}
				for _, col := range assetColumns {
					if str(old[col.Field]) != str(stored[col.Field]) {
						changes = append(changes, Row{"field": col.Field, "before": old[col.Field], "after": stored[col.Field]})
					}
				}
				writeEvent(ctx, tx, old, "导入", str(actor(r)["display_name"]), "导入批次 "+id+" · "+str(preview["file_name"]), nil, nil, changes)
			}
			exec(ctx, tx, "INSERT INTO import_batches (file_name,checksum,total_rows,new_rows,updated_rows,unchanged_rows,invalid_rows,errors_json) VALUES (?,?,?,?,?,?,?,?)", preview["file_name"], id, len(data), len(plan.New), len(plan.Updated), len(plan.Unchanged), len(plan.Errors), js(plan.Errors))
			exec(ctx, tx, "UPDATE import_previews SET result_json=? WHERE id=?", js(result), id)
			audit(ctx, tx, actor(r), "import", id, result)
			return result
		})
		send(w, 200, result)
	})
}
