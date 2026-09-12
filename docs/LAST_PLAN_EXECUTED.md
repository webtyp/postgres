---
PLAN: "fix: a NULL column scans as the Go zero value"
EXECUTOR: jules
REVIEWER: none
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.
>
> **Phase C** of
> [`NULLABLE_COLUMNS_MASTER_PLAN.md`](https://github.com/webtyp/docs/blob/main/NULLABLE_COLUMNS_MASTER_PLAN.md).
>
> **Depends on phase A** (`webtyp.com/storage` publishing `storage.NullSafe`):
> as the first line of work, `go get webtyp.com/storage@latest`. Never add a
> `replace`, never invent a version.

# Plan — `webtyp.com/postgres`: stop letting `database/sql` decide what NULL means

## 0. Context (verified against the repo — do not re-diagnose)

This backend hands the model's raw pointers (`*string`, `*int64`, …) straight to
`database/sql`, which refuses a NULL column:

```
sql: Scan error on column index 1, name "email": converting NULL to string is unsupported
```

Meanwhile `storage/mem` — the backend consumers unit-test against — returns the
Go zero value for the same row. So a consumer's suite passes and production
fails on the same code. This matters more here than anywhere: Postgres is where
a `UNIQUE` column is deliberately left NULL, because NULL is the value a unique
index allows to repeat.

Phase A settled the rule once, in the package that owns the contract:

```
A NULL column scans as the Go ZERO VALUE, in every backend.
```

Phase A also shipped the mechanism: `storage.NullSafe(dest)` wraps scan
destinations so `database/sql` hands the raw value to `storage.ScanAny` instead
of converting it itself. This phase applies it at this backend's two scan
seams. **No conversion logic is written here** — if a value converts wrongly,
the fix belongs in `storage.ScanAny`, not in this repo.

There are exactly two seams, both in `adapter.go`:

- `QueryRow` → returns `&errScanner{...}`, whose `Scan` forwards to `database/sql`.
- `Query` → returns `p.db.Query(...)` **directly**: a `*sql.Rows` satisfies
  `storage.Rows` structurally, so today there is no wrapper at all on the
  read-all path.

Check `tx.go` as well: it imports `database/sql` and, if it exposes its own
`QueryRow`/`Query`, it carries the identical defect and takes the identical fix.

This plan adds no public API. It changes behaviour only: a NULL that used to
error now yields the zero value.

## Quality rules

```
RULE: no conversion logic in this repo — every value goes through storage.ScanAny.
RULE: every repeated string is a named constant; string literals forbidden in logic.
RULE: do not change the ErrNoRows translation that errScanner already performs.
```

## Stage 1 — the single-row seam

**File:** `adapter.go`.

`errScanner.Scan` currently forwards the destinations untouched:

```go
func (e errScanner) Scan(dest ...any) error {
	err := e.s.Scan(dest...)
	if err == sql.ErrNoRows {
		return storage.ErrNoRows
	}
	return err
}
```

Wrap the destinations, keeping the `ErrNoRows` translation exactly as it is:

```go
func (e errScanner) Scan(dest ...any) error {
	err := e.s.Scan(storage.NullSafe(dest)...)
	if err == sql.ErrNoRows {
		return storage.ErrNoRows
	}
	return err
}
```

## Stage 2 — the read-all seam

**File:** `adapter.go`.

`Query` returns the `*sql.Rows` directly, so nothing applies the rule on this
path. Add a wrapper next to `errScanner`:

```go
// nullRows applies the storage contract's NULL rule to the read-all path.
// *sql.Rows satisfies storage.Rows on its own, which is precisely why this
// wrapper is easy to forget: without it, ReadAll answers differently from
// ReadOne on the same column.
type nullRows struct{ *sql.Rows }

func (r nullRows) Scan(dest ...any) error { return r.Rows.Scan(storage.NullSafe(dest)...) }
```

Embedding `*sql.Rows` keeps `Next`, `Close` and `Err` forwarding untouched;
only `Scan` is overridden.

Then return it from `Query`:

```go
func (p *PostgresAdapter) Query(query string, args ...any) (storage.Rows, error) {
	rows, err := p.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	return nullRows{rows}, nil
}
```

Apply the identical change to every other `Query`/`QueryRow` implementation in
the repo — check `tx.go` specifically.

**Do not** wrap the rows used by `introspect.go` / `tableColumns`: those scan
into local `*string` variables for `information_schema` output, not into a
model, and they are not part of the storage contract.

## Stage 3 — conformance proves it

**File:** the conformance test file in this repo (the one calling the exported
suite from `webtyp.com/storage/conformance`).

Phase A added a nullable column and a `NullScansAsZero` case to the shared
suite. Make sure this repo runs that suite and that the new case passes.

If the suite here is gated behind a live Postgres instance (an env var, a
docker service) and cannot run in the execution environment, **say so in the PR
description instead of deleting or skipping the case** — a skipped conformance
case is exactly how this defect survived.

## Acceptance criteria

1. `go build ./...`, `go vet ./...`, `go test ./...` green (or the Postgres-gated
   suite reported as unrunnable, per stage 3).
2. `grep -rn "\.Scan(dest\.\.\.)" --include='*.go' .` → no hit outside
   introspection helpers: every model-facing scan goes through `storage.NullSafe`.
3. `grep -rn "converting NULL" --include='*.go' .` → empty.
4. `go.mod` requires the phase A tag of `webtyp.com/storage`; no `replace`.

## Out of scope

- Conversion rules (`[]byte` → `string`, numeric text) — they live in
  `storage.ScanAny`, phase A.
- `webtyp/sqlite`, which carries the identical defect — phase B.

| Stage | Files | Action |
|---|---|---|
| 1 | `adapter.go` | `errScanner.Scan` wraps dest in `storage.NullSafe` |
| 2 | `adapter.go`, `tx.go` | `nullRows` wrapper returned by every `Query` |
| 3 | conformance test file | verify `NullScansAsZero` runs and passes |
