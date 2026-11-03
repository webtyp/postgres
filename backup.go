package postgres

import (
	"database/sql"
	"errors"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"webtyp.com/ddl"
	"webtyp.com/storage"
)

// Errors, verbatim.
var (
	ErrDSNNotURL        = errors.New("postgres: backups need a URL DSN (postgres://user:pass@host:port/db)")
	ErrPgRestoreMissing = errors.New("postgres: pg_restore not found in PATH — install the PostgreSQL client tools")
)

const (
	pgRestoreBin   = "pg_restore"
	snapshotPrefix = "snapshot_"
	urlSchemePG    = "postgres"
	urlSchemePGSQL = "postgresql"
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
	if _, err := exec.LookPath(pgRestoreBin); err != nil {
		return nil, ErrPgRestoreMissing
	}
	admin, err := sql.Open("postgres", b.dsn)
	if err != nil {
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
	out, err := exec.Command(pgRestoreBin, "--no-owner", "--no-acl", "--dbname", target.String(), path).CombinedOutput()
	if err != nil {
		return nil, errors.Join(errors.New("postgres: pg_restore: "+strings.TrimSpace(string(out))), drop())
	}
	raw, err := sql.Open("postgres", target.String())
	if err != nil {
		return nil, errors.Join(err, drop())
	}
	return &snapshot{PostgresAdapter: &PostgresAdapter{db: raw}, drop: drop}, nil
}

// snapshot is a restored archive: introspection and reads go through the
// adapter; Close closes it and drops the temporary database.
type snapshot struct {
	*PostgresAdapter
	drop func() error
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
	return s.PostgresAdapter.Query("SELECT " + strings.Join(list, ", ") + " FROM " + quoteIdent(table))
}

func (s *snapshot) Close() error {
	return errors.Join(s.PostgresAdapter.Close(), s.drop())
}

var _ ddl.Snapshot = (*snapshot)(nil)
