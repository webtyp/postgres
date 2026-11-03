package tests

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"webtyp.com/postgres"
)

// dsnOrSkip is from tests/conformance_test.go, redefining here since it's unexported
func backupDsnOrSkip(t *testing.T) string {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set (see docs/POSTGRES_SETUP.md)")
	}
	probe, err := postgres.Open(dsn)
	if err != nil {
		t.Skipf("POSTGRES_DSN set but unreachable: %v", err)
	}
	_ = probe.Close()
	return dsn
}

func makeArchive(t *testing.T, dsn string) string {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not in PATH")
	}

	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("makeArchive sql.Open: %v", err)
	}
	var serverMajor int
	if err := admin.QueryRow("SELECT current_setting('server_version_num')::int / 10000").Scan(&serverMajor); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}
	versionOut, err := exec.Command("pg_dump", "--version").Output()
	if err != nil {
		t.Skip("pg_dump not in PATH or fails")
	}
	idx := strings.Index(string(versionOut), "(PostgreSQL) ")
	if idx != -1 {
		verStr := string(versionOut)[idx+len("(PostgreSQL) "):]
		endIdx := strings.IndexAny(verStr, " .\n\r")
		if endIdx == -1 {
			endIdx = len(verStr)
		}
		clientMajor, err := strconv.Atoi(verStr[:endIdx])
		if err != nil {
			t.Logf("makeArchive Atoi failed: %v", err)
		} else {
			_ = clientMajor
		}
	}
	admin.Close()

	admin, err = sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("makeArchive sql.Open: %v", err)
	}
	n := strconv.FormatInt(time.Now().UnixNano(), 10)
	dbName := "backup_src_" + n
	if _, err := admin.Exec("CREATE DATABASE " + dbName); err != nil {
		t.Fatalf("makeArchive CREATE DATABASE: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + dbName + " WITH (FORCE)")
		admin.Close()
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("makeArchive url.Parse: %v", err)
	}
	u.Path = "/" + dbName
	targetDSN := u.String()

	raw, err := sql.Open("postgres", targetDSN)
	if err != nil {
		t.Fatalf("makeArchive sql.Open target: %v", err)
	}
	defer raw.Close()

	if _, err := raw.Exec(`CREATE TABLE staff (id text PRIMARY KEY, staff_name text, staff_credentials text)`); err != nil {
		t.Fatalf("makeArchive CREATE TABLE: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO staff VALUES ('1','Ana','1,2,3,4'),('2','O''Hara','3,4')`); err != nil {
		t.Fatalf("makeArchive INSERT: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "src.backup")
	out, err := exec.Command("pg_dump", "-Fc", "-f", backupPath, targetDSN).CombinedOutput()
	if err != nil {
		t.Fatalf("pg_dump failed: %v, out: %s", err, string(out))
	}
	return backupPath
}

func TestBackups_ReadsTables(t *testing.T) {
	dsn := backupDsnOrSkip(t)
	path := makeArchive(t, dsn)

	snap, err := postgres.Backups(dsn).OpenBackup(path)
	if err != nil {
		t.Fatalf("OpenBackup: %v", err)
	}
	defer snap.Close()

	tables, err := snap.Tables()
	if err != nil {
		t.Fatalf("Tables(): %v", err)
	}
	if len(tables) != 1 || tables[0] != "staff" {
		t.Errorf("Tables() = %v, want [staff]", tables)
	}
}

func TestBackups_Columns(t *testing.T) {
	dsn := backupDsnOrSkip(t)
	path := makeArchive(t, dsn)

	snap, err := postgres.Backups(dsn).OpenBackup(path)
	if err != nil {
		t.Fatalf("OpenBackup: %v", err)
	}
	defer snap.Close()

	cols, err := snap.Columns("staff")
	if err != nil {
		t.Fatalf("Columns(): %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("len(cols) = %d, want 3", len(cols))
	}
	if cols[0].Name != "id" || !cols[0].PK {
		t.Errorf("cols[0] = %+v, want name=id PK=true", cols[0])
	}
	if cols[1].Name != "staff_name" {
		t.Errorf("cols[1] = %+v, want name=staff_name", cols[1])
	}
	if cols[2].Name != "staff_credentials" {
		t.Errorf("cols[2] = %+v, want name=staff_credentials", cols[2])
	}
}

func TestBackups_ReadTable(t *testing.T) {
	dsn := backupDsnOrSkip(t)
	path := makeArchive(t, dsn)

	snap, err := postgres.Backups(dsn).OpenBackup(path)
	if err != nil {
		t.Fatalf("OpenBackup: %v", err)
	}
	defer snap.Close()

	rows, err := snap.ReadTable("staff")
	if err != nil {
		t.Fatalf("ReadTable(): %v", err)
	}
	defer rows.Close()

	var count int
	var lastStaffName string
	for rows.Next() {
		count++
		var id, name, creds any
		if err := rows.Scan(&id, &name, &creds); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if count == 2 {
			lastStaffName = name.(string)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err(): %v", err)
	}
	if count != 2 {
		t.Errorf("got %d rows, want 2", count)
	}
	if lastStaffName != "O'Hara" {
		t.Errorf("second row name = %q, want O'Hara", lastStaffName)
	}
}


func TestBackups_ReadTableCastsToText(t *testing.T) {
	dsn := backupDsnOrSkip(t)
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not in PATH")
	}

	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("makeArchive sql.Open: %v", err)
	}
	var serverMajor int
	if err := admin.QueryRow("SELECT current_setting('server_version_num')::int / 10000").Scan(&serverMajor); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}
	versionOut, err := exec.Command("pg_dump", "--version").Output()
	if err != nil {
		t.Skip("pg_dump not in PATH or fails")
	}
	idx := strings.Index(string(versionOut), "(PostgreSQL) ")
	if idx != -1 {
		verStr := string(versionOut)[idx+len("(PostgreSQL) "):]
		endIdx := strings.IndexAny(verStr, " .\n\r")
		if endIdx == -1 {
			endIdx = len(verStr)
		}
		clientMajor, err := strconv.Atoi(verStr[:endIdx])
		if err != nil {
			t.Logf("makeArchive Atoi failed: %v", err)
		} else {
			_ = clientMajor
		}
	}
	admin.Close()

	admin, err = sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	n := strconv.FormatInt(time.Now().UnixNano(), 10)
	dbName := "backup_src_" + n
	if _, err := admin.Exec("CREATE DATABASE " + dbName); err != nil {
		t.Fatalf("CREATE DATABASE: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + dbName + " WITH (FORCE)")
		admin.Close()
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	u.Path = "/" + dbName
	targetDSN := u.String()

	raw, err := sql.Open("postgres", targetDSN)
	if err != nil {
		t.Fatalf("sql.Open target: %v", err)
	}
	defer raw.Close()

	if _, err := raw.Exec(`CREATE TABLE t (id int PRIMARY KEY, d date, n text)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO t VALUES (7, '2024-05-10', NULL)`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "src.backup")
	out, err := exec.Command("pg_dump", "-Fc", "-f", backupPath, targetDSN).CombinedOutput()
	if err != nil {
		t.Fatalf("pg_dump failed: %v, out: %s", err, string(out))
	}

	snap, err := postgres.Backups(dsn).OpenBackup(backupPath)
	if err != nil {
		t.Fatalf("OpenBackup: %v", err)
	}
	defer snap.Close()

	rows, err := snap.ReadTable("t")
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatalf("expected 1 row")
	}
	var id, d, nilN any
	if err := rows.Scan(&id, &d, &nilN); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if id.(string) != "7" {
		t.Errorf("id = %q, want '7'", id.(string))
	}
	if d.(string) != "2024-05-10" {
		t.Errorf("d = %q, want '2024-05-10'", d.(string))
	}
	if nilN != nil {
		t.Errorf("nilN != nil, want nil")
	}
}

func TestBackups_ReadTableUnknown(t *testing.T) {
	dsn := backupDsnOrSkip(t)
	path := makeArchive(t, dsn)

	snap, err := postgres.Backups(dsn).OpenBackup(path)
	if err != nil {
		t.Fatalf("OpenBackup: %v", err)
	}
	defer snap.Close()

	_, err = snap.ReadTable("missing")
	if err == nil {
		t.Fatalf("ReadTable(missing) succeeded, want error")
	}
	if err.Error() != "postgres: table not in backup: missing" {
		t.Errorf("got error %q, want 'postgres: table not in backup: missing'", err.Error())
	}
}

func TestBackups_CloseDropsDatabase(t *testing.T) {
	dsn := backupDsnOrSkip(t)
	path := makeArchive(t, dsn)

	snap, err := postgres.Backups(dsn).OpenBackup(path)
	if err != nil {
		t.Fatalf("OpenBackup: %v", err)
	}

	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("admin sql.Open: %v", err)
	}
	defer admin.Close()

	checkDB := func() bool {
		rows, err := admin.Query(`SELECT datname FROM pg_database WHERE datname LIKE 'snapshot_%'`)
		if err != nil {
			t.Fatalf("admin.Query: %v", err)
		}
		defer rows.Close()
		return rows.Next()
	}

	if !checkDB() {
		t.Errorf("expected snapshot_ database to exist before Close")
	}

	if err := snap.Close(); err != nil {
		t.Fatalf("snap.Close(): %v", err)
	}

	if checkDB() {
		t.Errorf("expected snapshot_ database to be dropped after Close")
	}
}

func TestBackups_RejectsKeyValueDSN(t *testing.T) {
	_, err := postgres.Backups("host=x dbname=y").OpenBackup("x")
	if !errors.Is(err, postgres.ErrDSNNotURL) {
		t.Errorf("got err %v, want ErrDSNNotURL", err)
	}
}

func TestBackups_BadArchive(t *testing.T) {
	dsn := backupDsnOrSkip(t)

	badPath := filepath.Join(t.TempDir(), "bad.backup")
	if err := os.WriteFile(badPath, []byte("not an archive"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	snap, err := postgres.Backups(dsn).OpenBackup(badPath)
	if err == nil {
		if snap != nil {
			snap.Close()
		}
		t.Fatalf("OpenBackup succeeded with bad archive, want error")
	}

	if !strings.Contains(err.Error(), "pg_restore") {
		t.Errorf("expected error containing pg_restore, got %q", err.Error())
	}

	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("admin sql.Open: %v", err)
	}
	defer admin.Close()

	rows, err := admin.Query(`SELECT datname FROM pg_database WHERE datname LIKE 'snapshot_%'`)
	if err != nil {
		t.Fatalf("admin.Query: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Errorf("expected no snapshot_* database left")
	}
}

func TestBackups_NoticeWhenClientNewer(t *testing.T) {
	dsn := backupDsnOrSkip(t)

	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer admin.Close()
	var serverMajor int
	if err := admin.QueryRow("SELECT current_setting('server_version_num')::int / 10000").Scan(&serverMajor); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}

	versionOut, err := exec.Command("pg_dump", "--version").Output()
	if err != nil {
		t.Skip("pg_dump not in PATH or fails")
	}
	idx := strings.Index(string(versionOut), "(PostgreSQL) ")
	if idx == -1 {
		t.Skip("pg_dump output format unknown")
	}
	verStr := string(versionOut)[idx+len("(PostgreSQL) "):]
	endIdx := strings.IndexAny(verStr, " .\n\r")
	if endIdx == -1 {
		endIdx = len(verStr)
	}
	clientMajor, err := strconv.Atoi(verStr[:endIdx])
	if err != nil {
		t.Skipf("Atoi: %v", err)
	}

	path := makeArchive(t, dsn)
	snap, err := postgres.Backups(dsn).OpenBackup(path)
	if err != nil {
		t.Fatalf("OpenBackup: %v", err)
	}
	defer snap.Close()

	notices := snap.Notices()
	if clientMajor > serverMajor {
		if len(notices) != 1 {
			t.Fatalf("expected 1 notice, got %d: %v", len(notices), notices)
		}
		if !strings.Contains(notices[0].Error(), "transaction_timeout") {
			t.Errorf("notice does not contain transaction_timeout: %v", notices[0])
		}
	} else {
		if len(notices) != 0 {
			t.Errorf("expected 0 notices, got %d: %v", len(notices), notices)
		}
	}
}
