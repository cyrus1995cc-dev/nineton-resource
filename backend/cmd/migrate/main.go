// migrate initializes MySQL or copies an existing SQLite database into an empty MySQL database.
// Source SQLite is always opened read-only; existing destination data is never replaced.
package main

import (
	"asset-system/backend/internal/app"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func die(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func main() {
	source := flag.String("sqlite", "", "existing SQLite database to import read-only")
	flag.Parse()
	db, e := app.Open(os.Getenv("MYSQL_DSN"), 2)
	die(e)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	die(app.Migrate(ctx, db))
	if *source == "" {
		fmt.Println("MySQL schema ready")
		return
	}
	absolute, e := filepath.Abs(*source)
	die(e)
	if _, e = os.Stat(absolute); e != nil {
		die(e)
	}
	sourcePath := filepath.ToSlash(absolute)
	if filepath.VolumeName(absolute) != "" {
		sourcePath = "/" + sourcePath
	}
	uri := url.URL{Scheme: "file", Path: sourcePath}
	src, e := sql.Open("sqlite", uri.String()+"?mode=ro")
	die(e)
	defer src.Close()
	read, e := src.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	die(e)
	defer read.Rollback()
	var integrity string
	die(read.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity))
	if integrity != "ok" {
		die(fmt.Errorf("source integrity check: %s", integrity))
	}
	tx, e := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	die(e)
	defer tx.Rollback()
	for _, table := range append(app.Tables(), "sessions") {
		var n int
		die(tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+table+"`").Scan(&n))
		if n != 0 {
			die(fmt.Errorf("destination table %s is not empty; use a fresh database", table))
		}
	}
	for _, table := range app.Tables() {
		if table == "session_policies" {
			continue
		} // Sessions are intentionally never migrated.
		if table == "companies" || table == "employee_departments" || table == "company_names" || table == "external_identities" || strings.HasPrefix(table, "asset_card_") || table == "asset_categories" || table == "asset_code_rules" || table == "asset_profiles" || table == "asset_create_requests" {
			var exists int
			die(read.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists))
			if exists == 0 {
				continue
			}
		}
		rs, e := read.QueryContext(ctx, "SELECT * FROM `"+table+"` ORDER BY "+order(table))
		die(e)
		columns, e := rs.Columns()
		die(e)
		quoted := make([]string, len(columns))
		marks := make([]string, len(columns))
		for i, c := range columns {
			quoted[i] = "`" + c + "`"
			marks[i] = "?"
		}
		stmt, e := tx.PrepareContext(ctx, "INSERT INTO `"+table+"` ("+strings.Join(quoted, ",")+") VALUES ("+strings.Join(marks, ",")+")")
		die(e)
		var n int64
		for rs.Next() {
			values := make([]any, len(columns))
			ptr := make([]any, len(columns))
			for i := range values {
				ptr[i] = &values[i]
			}
			die(rs.Scan(ptr...))
			_, e = stmt.ExecContext(ctx, values...)
			if e != nil {
				die(fmt.Errorf("copy %s row %d: %w", table, n+1, e))
			}
			n++
		}
		die(rs.Err())
		rs.Close()
		stmt.Close()
		var actual int64
		die(tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+table+"`").Scan(&actual))
		if actual != n {
			die(fmt.Errorf("count mismatch for %s", table))
		}
		fmt.Printf("%s: %d rows verified\n", table, n)
	}
	var copiedHash, sourceHash string
	sourceRows, e := read.QueryContext(ctx, "SELECT id,password_hash FROM users ORDER BY id")
	die(e)
	for sourceRows.Next() {
		var id int64
		die(sourceRows.Scan(&id, &sourceHash))
		die(tx.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id=?", id).Scan(&copiedHash))
		if sourceHash != copiedHash {
			die(fmt.Errorf("password hash mismatch for user %d", id))
		}
	}
	die(sourceRows.Err())
	sourceRows.Close()
	die(tx.Commit())
	app.SeedCompanies(ctx, db)
	fmt.Println("Migration committed; IDs, QR data, password hashes and history preserved. Sessions intentionally omitted; sign in again.")
}
func order(table string) string {
	if table == "external_identities" {
		return "`issuer`,`subject`"
	}
	if table == "company_names" {
		return "`name`"
	}
	if table == "system_settings" {
		return "`key`"
	}
	return "id"
}
