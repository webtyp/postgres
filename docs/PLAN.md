---
PLAN: "feat(postgres): render ddl.OpCopyColumn; update to ddl v0.0.45"
EXECUTOR: jules
REVIEWER: none
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.

# webtyp/postgres — copy one column into another

## Why

`webtyp.com/ddl` v0.0.45 added `ddl.OpCopyColumn`: before a column a model no longer has is dropped, its values are
copied into the column that replaced it, only where that column is still empty. `ddl/conformance` now has two new
clauses every SQL backend must pass (`copy_column_fills_only_empty_rows`, `drop_column_drops_a_column_with_data`).
Today `translateDDL` answers `postgres: unknown DDL op` for it.

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
go install webtyp.com/lang/cmd/langc@latest
export GOPRIVATE=webtyp.com/*,github.com/veltylabs/* GOFLAGS=-mod=mod
go get webtyp.com/ddl@v0.0.45 && go mod tidy
```
Measured before this plan: with `ddl v0.0.45`, `go vet ./...` and `go test ./...` pass unchanged (the engine
conformance skips without `POSTGRES_DSN`).

## APIs used

| Symbol | Exact signature | Tag | File |
|---|---|---|---|
| `ddl.OpCopyColumn` | `ddl.Op` constant, after `OpDropColumn` | `webtyp.com/ddl v0.0.45` | `compiler.go` |
| `ddl.Stmt` | `struct{ Op Op; Table string; Database string; Column *model.Field; OldName string; ColumnName string }` — for `OpCopyColumn`: `OldName` = source column, `ColumnName` = destination column | v0.0.45 | `compiler.go` |
| `postgres.NewCompiler` | `func NewCompiler() *Compiler`; `(*Compiler).CompileDDL(s ddl.Stmt, m model.Model) (string, []any, error)` | this repo | `compiler.go` |

In this repo: `quoteIdent(name string) string` (`translate.go`).

If a signature in the published tag differs from this table, stop and name the symbol in the PR; never adapt it by guessing.

## Stage 1 — Red tests

New file `tests/copy_column_test.go` (`package tests`, same package name as the other files in `tests/`):

```go
package tests

import (
	"testing"

	"webtyp.com/ddl"
	"webtyp.com/postgres"
)

func TestCopyColumn_RendersUpdateOnlyForEmptyRows(t *testing.T) {
	sql, args, err := postgres.NewCompiler().CompileDDL(ddl.Stmt{
		Op:         ddl.OpCopyColumn,
		Table:      "holiday",
		OldName:    "label",
		ColumnName: "name",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `UPDATE "holiday" SET "name" = "label" WHERE "name" IS NULL AND "label" IS NOT NULL`
	if sql != want || len(args) != 0 {
		t.Fatalf("got %q %v, want %q", sql, args, want)
	}
}

func TestCopyColumn_MissingNameFails(t *testing.T) {
	_, _, err := postgres.NewCompiler().CompileDDL(ddl.Stmt{Op: ddl.OpCopyColumn, Table: "holiday", OldName: "label"}, nil)
	if err == nil {
		t.Fatal("expected an error when the destination column is empty")
	}
}
```
Commit.

## Stage 2 — Code

`translate.go`, in `translateDDL`, add before `default:`:
```go
	case ddl.OpCopyColumn:
		if s.Table == "" || s.OldName == "" || s.ColumnName == "" {
			return "", nil, fmt.Err("table, old name and column", "required for copy column")
		}
		sb.Write("UPDATE ")
		sb.Write(quoteIdent(s.Table))
		sb.Write(" SET ")
		sb.Write(quoteIdent(s.ColumnName))
		sb.Write(" = ")
		sb.Write(quoteIdent(s.OldName))
		sb.Write(" WHERE ")
		sb.Write(quoteIdent(s.ColumnName))
		sb.Write(" IS NULL AND ")
		sb.Write(quoteIdent(s.OldName))
		sb.Write(" IS NOT NULL")
```
So the phrase is shared (reuse rule), change the `OpRenameColumn` error to
`fmt.Err("table, old name and column", "required for rename")`.
Grep: `grep -n '"table, old name and column required for rename"' translate.go` → empty.

If `POSTGRES_DSN` is reachable in your environment (`docs/POSTGRES_SETUP.md`), run
`go test ./tests/ -run DDLConformance -v`: both new clauses must pass. Without it they skip; the unit tests above are
then the evidence.

`gotest` green. Commit.

## Stage 3 — Translations

`langc sync .`; fill every empty value it reports in `lang.json` (`es`, `zh`, `hi`, `ar`, `pt`, `fr`, `de`, `ru` —
several existing keys have only `es`: fill them too). Remove nothing by hand; `langc` drops keys no longer used. Zero
untranslated keys and zero reuse warnings. Commit.

## Stage 4 — Documentation

- `docs/ARCHITECTURE.md`: where DDL ops are listed, add `OpCopyColumn` → `UPDATE … SET dest = src WHERE dest IS NULL
  AND src IS NOT NULL` (one line).
- `README.md` to the template (≤ 150 lines), four sections in order: `## Why This Exists` (two sentences + a
  Without/With table: without, hand-written PostgreSQL per migration; with, `ddl.Sync`/`CopyColumn`/`DropColumn` and
  `storage` queries over one `postgres.Open` connection; inline links to [ARCHITECTURE](docs/ARCHITECTURE.md) and
  [POSTGRES_SETUP](docs/POSTGRES_SETUP.md)), `## Before You Start` (a PostgreSQL server and its DSN),
  `## Step-by-Step Guide` (complete snippets: `conn, err := postgres.Open(dsn)`; `ddl.New(conn, conn.(ddl.Compiler)).Sync(&Model{})`;
  `CopyColumn` + `DropColumn`; keep whatever backup guidance the current README has as a sub-step), `## Documentation Map`
  (table of `docs/*` files and what each answers). Keep the existing content that still applies; link source files
  instead of listing constants.

Commit.

## Delivery check

1. `gotest` passes. A PR that does not compile, or whose tests could not run, is a failed delivery.
2. `langc sync .` → zero untranslated keys, zero reuse warnings.
3. `git diff --stat main` lists: `go.mod`, `go.sum`, `translate.go`, `tests/copy_column_test.go`, `lang.json`,
   `docs/ARCHITECTURE.md`, `README.md`.

Delivery rules: never pause to ask or report progress — when the check passes, commit, push and open the PR; stuck 30
minutes, open the PR listing what fails. Commit after every stage; never `git reset`, `git restore`,
`git checkout -- <file>` or `git stash` on your own work.

## Stages

| Stage | What | Files |
|---|---|---|
| 0 | Upgrade `ddl` | `go.mod`, `go.sum` |
| 1 | Red tests | `tests/copy_column_test.go` |
| 2 | Code | `translate.go` |
| 3 | Translations | `lang.json` |
| 4 | Docs | `docs/ARCHITECTURE.md`, `README.md` |
