package postgres

import (
	"database/sql"
	"errors"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"webtyp.com/ddl"
	wfmt "webtyp.com/fmt"
	"webtyp.com/storage"
)

// Errors, verbatim.
var (
	ErrDSNNotURL        = errors.New("postgres: backups need a URL DSN (postgres://user:pass@host:port/db)")
	ErrPgRestoreMissing = errors.New("postgres: pg_restore not found in PATH — install the PostgreSQL client tools")
	ErrPgRestoreNewer   = errors.New("postgres: pg_restore is newer than the server — install the client tools of the server's major version and put them first in PATH")
)

const (
	pgRestoreBin = "pg_restore"
	// settingTransactionTimeout is the only setting a newer pg_restore sends that an older server rejects; passed
	// as data in the notice, never as a word to translate.
	settingTransactionTimeout = "transaction_timeout"
	snapshotPrefix            = "snapshot_"
	urlSchemePG               = "postgres"
	urlSchemePGSQL            = "postgresql"
)

// Backups opens pg_dump archives by restoring each one into a temporary database
// on the server dsn points to. Each Snapshot drops its database on Close.
//
// Example:
//
//	snap, err := postgres.Backups(dsn).OpenBackup("migrate/pastdb/1793656810107289500-db.backup")
//	if err != nil { return err }
//	defer snap.Close()
//	rows, err := snap.ReadTable("staff")
func Backups(dsn string) ddl.BackupFormat { return backups{dsn: dsn} }

type backups struct{ dsn string }

func (b backups) OpenBackup(path string) (ddl.Snapshot, error) {
	u, err := url.Parse(b.dsn)
	if err != nil || (u.Scheme != urlSchemePG && u.Scheme != urlSchemePGSQL) {
		return nil, ErrDSNNotURL
	}
	restoreTool, err := exec.LookPath(pgRestoreBin)
	if err != nil {
		return nil, ErrPgRestoreMissing
	}
	admin, err := sql.Open("postgres", b.dsn)
	if err != nil {
		return nil, err
	}
	var serverMajor int
	if err := admin.QueryRow("SELECT current_setting('server_version_num')::int / 10000").Scan(&serverMajor); err != nil {
		admin.Close()
		return nil, err
	}
	versionOut, err := exec.Command(pgRestoreBin, "--version").Output()
	if err != nil {
		admin.Close()
		return nil, err
	}
	clientMajor, newer, err := newerClient(string(versionOut), serverMajor)
	if err != nil {
		admin.Close()
		return nil, err
	}
	name := snapshotPrefix + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := admin.Exec("CREATE DATABASE " + quoteIdent(name)); err != nil {
		admin.Close()
		return nil, err
	}
	target := *u
	target.Path = "/" + name
	drop := func() error {
		_, err := admin.Exec("DROP DATABASE IF EXISTS " + quoteIdent(name) + " WITH (FORCE)")
		return errors.Join(err, admin.Close())
	}
	out, err := exec.Command(pgRestoreBin, "--clean", "--if-exists", "--no-owner", "--no-acl", "--dbname", target.String(), path).CombinedOutput()
	var notices []error
	if err != nil {
		if newer && onlyTransactionTimeout(string(out)) {
			tool := filepath.Base(restoreTool) // the tool name is data: never a word to translate
			notices = []error{wfmt.Err(tool, clientMajor, "is newer than the database server", serverMajor, "— the server did not understand one setting it sent", "("+settingTransactionTimeout+").", "It was skipped: your data was restored completely.", "To stop seeing this notice, install the PostgreSQL client tools of version", serverMajor)}
		} else {
			return nil, errors.Join(errors.New("postgres: pg_restore: "+strings.TrimSpace(string(out))), drop())
		}
	}
	raw, err := sql.Open("postgres", target.String())
	if err != nil {
		return nil, errors.Join(err, drop())
	}
	return &snapshot{PostgresAdapter: &PostgresAdapter{db: raw}, drop: drop, notices: notices}, nil
}

// snapshot is a restored archive: introspection and reads go through the
// adapter; Close closes it and drops the temporary database.
type snapshot struct {
	*PostgresAdapter
	drop    func() error
	notices []error
}

// ReadTable casts every column to text (ddl.Snapshot contract): the consumer
// scans every engine into *sql.NullString the same way.
func (s *snapshot) ReadTable(table string) (storage.Rows, error) {
	cols, err := s.Columns(table)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, errors.New("postgres: table not in backup: " + table)
	}
	list := make([]string, len(cols))
	for i, c := range cols {
		q := quoteIdent(c.Name)
		list[i] = q + "::text AS " + q
	}
	return s.Query("SELECT " + strings.Join(list, ", ") + " FROM " + quoteIdent(table))
}

func (s *snapshot) Close() error {
	return errors.Join(s.PostgresAdapter.Close(), s.drop())
}

func (s *snapshot) Notices() []error {
	return s.notices
}

var _ ddl.Snapshot = (*snapshot)(nil)

func newerClient(clientVersionOutput string, serverMajor int) (client int, newer bool, err error) {
	idx := strings.Index(clientVersionOutput, "(PostgreSQL) ")
	if idx == -1 {
		return 0, false, errors.New("pg_restore output format unknown")
	}
	verStr := clientVersionOutput[idx+len("(PostgreSQL) "):]
	endIdx := strings.IndexAny(verStr, " .\n\r")
	if endIdx == -1 {
		endIdx = len(verStr)
	}
	clientMajor, err := strconv.Atoi(verStr[:endIdx])
	if err != nil {
		return 0, false, err
	}
	return clientMajor, clientMajor > serverMajor, nil
}

func onlyTransactionTimeout(output string) bool {
	if output == "" {
		return false
	}
	lines := strings.Split(output, "\n")
	hasError := false
	for _, line := range lines {
		if strings.Contains(line, "error:") {
			if strings.Contains(line, "errors ignored on restore:") {
				continue
			}
			if !strings.Contains(line, "unrecognized configuration parameter \"transaction_timeout\"") {
				return false
			}
			hasError = true
		}
	}
	return hasError
}
