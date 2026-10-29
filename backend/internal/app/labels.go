package app

import (
	"context"
	"database/sql"
	"math"
	"net/http"
	"strconv"
)

const labelSettingsKey = "labels.templates.v1"

type LabelTemplate struct {
	ID       string   `json:"id"`
	Version  int64    `json:"version"`
	Name     string   `json:"name"`
	Width    float64  `json:"width"`
	Height   float64  `json:"height"`
	QRSize   float64  `json:"qrSize"`
	Padding  float64  `json:"padding"`
	FontSize float64  `json:"fontSize"`
	Title    string   `json:"title"`
	Fields   []string `json:"fields"`
}

func defaultLabelTemplates() []LabelTemplate {
	return []LabelTemplate{{ID: "standard", Version: 1, Name: "YLF-280 · 50×30 标准标签", Width: 50, Height: 30, QRSize: 20, Padding: 2, FontSize: 7, Title: "", Fields: []string{"name", "asset_code"}}}
}

func labelTemplates(ctx context.Context, q DBTX, lock bool) []LabelTemplate {
	query := "SELECT value FROM system_settings WHERE `key`=?"
	if lock {
		query += " FOR UPDATE"
	}
	record := one(ctx, q, query, labelSettingsKey)
	if record == nil {
		return defaultLabelTemplates()
	}
	var templates []LabelTemplate
	decode(str(record["value"]), &templates)
	return templates
}

func labelNumber(b Row, key string, min, max float64) float64 {
	value, err := strconv.ParseFloat(str(b[key]), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < min || value > max {
		fail(400, "标签尺寸、字号或边距超出允许范围")
	}
	return math.Round(value*10) / 10
}

func parseLabelTemplate(b Row) LabelTemplate {
	t := LabelTemplate{Name: text(b["name"], 60, true), Width: labelNumber(b, "width", 40, 100), Height: labelNumber(b, "height", 25, 80), QRSize: labelNumber(b, "qrSize", 16, 40), Padding: labelNumber(b, "padding", 1, 4), FontSize: labelNumber(b, "fontSize", 6, 12), Title: text(b["title"], 20, false)}
	if t.QRSize+2*t.Padding > t.Height || t.Width-2*t.Padding-t.QRSize-1 < 16 {
		fail(400, "二维码尺寸过大，需为文字和边距留出空间")
	}
	raw, ok := b["fields"].([]any)
	if !ok || len(raw) < 2 || len(raw) > 4 {
		fail(400, "请选择2至4个标签字段")
	}
	seen := map[string]bool{}
	for _, v := range raw {
		field := str(v)
		if !contains([]string{"name", "asset_code", "company", "model", "location", "department"}, field) || seen[field] {
			fail(400, "标签字段无效或重复")
		}
		seen[field] = true
		t.Fields = append(t.Fields, field)
	}
	if !seen["name"] || !seen["asset_code"] {
		fail(400, "标签必须包含资产名称和编码")
	}
	return t
}

func (a *App) listLabelTemplates(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	send(w, 200, Row{"templates": labelTemplates(r.Context(), a.DB, false)})
}

func (a *App) saveLabelTemplate(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	b := body(r)
	input := parseLabelTemplate(b)
	id := r.PathValue("id")
	var expected int64
	if id != "" {
		expected = integer(b["expectedVersion"], 0, 9007199254740991)
	}
	result := transaction(r.Context(), a.DB, func(tx *sql.Tx) LabelTemplate {
		// Serialize initial creation as well as updates; reads need no database writes.
		exec(r.Context(), tx, "INSERT INTO system_settings (`key`,value) VALUES (?,?) ON DUPLICATE KEY UPDATE `key`=`key`", labelSettingsKey, js(defaultLabelTemplates()))
		templates := labelTemplates(r.Context(), tx, true)
		index := -1
		for i, t := range templates {
			if t.ID == id {
				index = i
			}
			if t.Name == input.Name && t.ID != id {
				fail(409, "标签样式名称已存在")
			}
		}
		if id == "" {
			if len(templates) >= 20 {
				fail(400, "最多保存20个标签样式")
			}
			input.ID = randomID()
			input.Version = 1
			templates = append(templates, input)
		} else {
			if index < 0 {
				fail(404, "标签样式不存在")
			}
			if templates[index].Version != expected {
				fail(409, "标签样式已被修改，请刷新后重试")
			}
			input.ID = id
			input.Version = expected + 1
			templates[index] = input
		}
		exec(r.Context(), tx, "UPDATE system_settings SET value=? WHERE `key`=?", js(templates), labelSettingsKey)
		audit(r.Context(), tx, actor(r), "label_template_save", input.ID, input)
		return input
	})
	status := 200
	if id == "" {
		status = 201
	}
	send(w, status, Row{"template": result})
}

func (a *App) deleteLabelTemplate(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin")
	version := integer(body(r)["expectedVersion"], 0, 9007199254740991)
	transaction(r.Context(), a.DB, func(tx *sql.Tx) bool {
		templates := labelTemplates(r.Context(), tx, true)
		index := -1
		for i, t := range templates {
			if t.ID == r.PathValue("id") {
				index = i
			}
		}
		if index < 0 {
			fail(404, "标签样式不存在")
		}
		if len(templates) == 1 {
			fail(409, "请至少保留一个标签样式")
		}
		if templates[index].Version != version {
			fail(409, "标签样式已被修改，请刷新后重试")
		}
		removed := templates[index]
		templates = append(templates[:index], templates[index+1:]...)
		exec(r.Context(), tx, "UPDATE system_settings SET value=? WHERE `key`=?", js(templates), labelSettingsKey)
		audit(r.Context(), tx, actor(r), "label_template_delete", removed.ID, removed)
		return true
	})
	send(w, 200, Row{"ok": true})
}

func (a *App) labelAssets(w http.ResponseWriter, r *http.Request) {
	roles(r, "admin", "asset_manager")
	b := body(r)
	raw, ok := b["assetCodes"].([]any)
	if !ok || len(raw) < 1 || len(raw) > 100 {
		fail(400, "请选择1至100件资产打印")
	}
	seen := map[string]bool{}
	assets := []Row{}
	// Read current printable values in one snapshot; printing never changes the ledger or old QR.
	transaction(r.Context(), a.DB, func(tx *sql.Tx) bool {
		for _, v := range raw {
			code := text(v, 100, true)
			if !codePattern.MatchString(code) || seen[code] {
				fail(400, "资产编码无效或重复")
			}
			seen[code] = true
			asset := one(r.Context(), tx, "SELECT asset_code,name,company,model,location,department FROM assets WHERE asset_code=?", code)
			if asset == nil {
				fail(409, "所选资产已变化，请刷新清单后重新选择")
			}
			assets = append(assets, asset)
		}
		return true
	})
	send(w, 200, Row{"assets": assets})
}
