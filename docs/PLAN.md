---
PLAN: "feat: postgres.Backups — open a pg_dump archive as a read-only ddl.Snapshot"
EXECUTOR: jules
REVIEWER: none
STATUS: review
SESSION: 10186233486079692440
PR: https://github.com/webtyp/postgres/pull/17
REVIEWED_COMMIT: ae47b66011eb4241a357bd463ebdea17f4d6e260
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.

# Plan — `postgres.Backups`: read a `pg_dump` archive through `ddl.Snapshot`

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
go get webtyp.com/ddl@v0.0.39
```

Environment: `GOPRIVATE=webtyp.com/*,github.com/veltylabs/*`, `GOFLAGS=-mod=mod`, run `go mod tidy` after `go get`.

## Why

An app that replaces a legacy system must read that system's backup (a `pg_dump -Fc` archive, "custom format":
binary and compressed). `webtyp.com/ddl@v0.0.39` added the engine-neutral contract (`ddl.BackupFormat`,
`ddl.Snapshot`); this repo implements it for PostgreSQL. First consumer: `webtyp.com/dataimport`.

## Ecosystem search

- `ddl.BackupFormat` / `ddl.Snapshot` (ddl v0.0.39, `backup.go`) → **implement**.
- `tables()` / `columns()` in `introspect.go` already answer `SchemaInspector` → **reuse**.
- No repo restores a pg_dump archive. `webtyp/selfupdate/compose.go` runs `pg_dump` inside Docker to *write* a
  backup — different concern, untouched.

## Design gate

1. **Prior art.** `pg_restore` (https://www.postgresql.org/docs/current/app-pgrestore.html) is the only reader of
   the custom format: restoring into a scratch database and reading it with SQL is the documented path. Tools like
   pgloader and AWS DMS do the same (restore/attach, then read) — unverified. Followed.
2. **Names.** `postgres.Backups(dsn)` — "the backups of this server". `OpenBackup`, `ReadTable`, `Close` come from ddl.
3. **Ledger.** Concepts +1 (`Backups`); ways to read a backup 0 → 1.
4. **Where.** PostgreSQL-specific mechanics (pg_restore, CREATE/DROP DATABASE) live only here.
5. **Deletes.** Nothing: new capability.

## APIs used

| Symbol | Signature | Tag | File |
|---|---|---|---|
| `ddl.BackupFormat` | `interface { OpenBackup(path string) (ddl.Snapshot, error) }` | ddl v0.0.39 | `backup.go` |
| `ddl.Snapshot` | `interface { Tables() ([]string, error); Columns(table string) ([]ddl.ColumnInfo, error); ReadTable(table string) (storage.Rows, error); Close() error }` | ddl v0.0.39 | `backup.go` |
| `storage.Rows` | `interface { Next() bool; Scan(dest ...any) error; Columns() ([]string, error); Close() error; Err() error }` | storage v0.1.18 | `executor.go` |
| `tables(q querier)`, `columns(q querier, table string)` | unexported, this repo | — | `introspect.go` |
| `quoteIdent(name string) string` | unexported, this repo | — | `translate.go` |

If a signature in the published tag differs from this table, stop and name the symbol in the PR; never adapt it by guessing.

## Stage 1 — `backup.go` (new file)

```go
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
```

Anti-footguns:
- This file is backend-only by nature (`os/exec`); the repo already uses stdlib (`database/sql`) — do NOT replace
  `errors`/`strings`/`strconv`/`time` with webtyp equivalents here.
- `pg_restore` may print warnings and exit non-zero for objects owned by roles that do not exist; `--no-owner
  --no-acl` prevents that. Do not add `--exit-on-error=false` or ignore the exit code.
- `snapshot` embeds `*PostgresAdapter` so `Tables`/`Columns` come from `introspect.go`; do not re-implement them.

## Stage 2 — `tests/backup_test.go` (new file)

Same skip rule as `tests/conformance_test.go` (`dsnOrSkip`). Each test needs a real archive, built inside the test:

```go
func makeArchive(t *testing.T, dsn string) string // creates db "backup_src_<nano>", runs:
//   CREATE TABLE staff (id text PRIMARY KEY, staff_name text, staff_credentials text);
//   INSERT INTO staff VALUES ('1','Ana','1,2,3,4'),('2','O''Hara','3,4');
// then `pg_dump -Fc -f <t.TempDir()>/src.backup <dsn of that db>`, drops the source db, returns the file path.
// Skip with t.Skip("pg_dump not in PATH") when exec.LookPath("pg_dump") fails.
```

| Test | Setup → call | Expected |
|---|---|---|
| `TestBackups_ReadsTables` | archive → `OpenBackup(path)` → `Tables()` | `[]string{"staff"}` |
| `TestBackups_Columns` | same → `Columns("staff")` | names `id, staff_name, staff_credentials`; `id` has `PK == true` |
| `TestBackups_ReadTable` | same → `ReadTable("staff")`, scan into three `sql.NullString` | 2 rows; second `staff_name.String == "O'Hara"` |
| `TestBackups_ReadTableCastsToText` | archive with `CREATE TABLE t (id int PRIMARY KEY, d date, n text)` and row `(7, '2024-05-10', NULL)` → `ReadTable("t")` into three `sql.NullString` | `"7"`, `"2024-05-10"`, `Valid == false` |
| `TestBackups_ReadTableUnknown` | `ReadTable("missing")` | error containing `table not in backup: missing` |
| `TestBackups_CloseDropsDatabase` | open, record `current_database()` via `ReadTable`-free query on a separate admin conn listing `pg_database` names with prefix `snapshot_` before/after `Close()` | the snapshot database exists before, not after |
| `TestBackups_RejectsKeyValueDSN` | `postgres.Backups("host=x dbname=y").OpenBackup("x")` (no DB needed, not skipped) | `errors.Is(err, postgres.ErrDSNNotURL)` |
| `TestBackups_BadArchive` | write `"not an archive"` to a temp file → `OpenBackup` | error containing `pg_restore`; no `snapshot_*` database left |

## Stage 3 — documentation

- `README.md` (keep ≤ 150 lines): add section `## Reading a backup` after Usage, with the Example of `Backups`
  above (complete, compilable, with `dsn` declared) and one line: needs `pg_restore` in PATH and a URL DSN with
  permission to `CREATE DATABASE`.
- `docs/ARCHITECTURE.md` (create if missing, 4 sections per the documentation skill: Purpose, Components,
  Flows, Decisions): the `OpenBackup` flow (create temp db → pg_restore → adapter → Close drops) and the decision
  "restore then read with SQL; never parse the custom format".

## Delivery check

1. `gotest` passes (a PR that does not compile, or whose tests could not run, is a failed delivery).
2. `grep -n "func Backups" backup.go` → one line.
3. `git diff --stat main` lists `backup.go`, `tests/backup_test.go`, `README.md`, `docs/ARCHITECTURE.md`, `go.mod`, `go.sum`.

Commit after every stage. Never run `git reset`, `git restore`, `git checkout -- <file>` or `git stash` on your own
work. Stuck 30 minutes: open the PR anyway listing what fails.

## Stages

| Stage | Files | Done when |
|---|---|---|
| 1 | `backup.go`, `go.mod`, `go.sum` | compiles against ddl v0.0.39 |
| 2 | `tests/backup_test.go` | the eight tests pass (or skip without POSTGRES_DSN, except the DSN one) |
| 3 | `README.md`, `docs/ARCHITECTURE.md` | sections written |
