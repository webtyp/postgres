# PostgreSQL Adapter for webtyp/storage + ddl
<img src="docs/img/badges.svg">

This repository implements the `storage.Conn` (from `webtyp.com/storage`) and `ddl.Compiler` (from `webtyp.com/ddl`) interfaces for PostgreSQL.

## Usage

Instead of a global registry or init-driven registration, the PostgreSQL adapter is constructed explicitly:

```go
package main

import (
	"log"

	"webtyp.com/postgres"
)

func main() {
	dsn := "postgres://user:password@localhost:5432/dbname?sslmode=disable"

	// Open returns a storage.Conn
	conn, err := postgres.Open(dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// Use conn...
}
```


## Reading a backup

```go
	snap, err := postgres.Backups(dsn).OpenBackup("migrate/pastdb/1793656810107289500-db.backup")
	if err != nil { return err }
	defer snap.Close()
	rows, err := snap.ReadTable("staff")
```

Needs `pg_restore` in PATH and a URL DSN with permission to `CREATE DATABASE`. The PostgreSQL client tools must not be newer than the server (ErrPgRestoreNewer says which to install).

## Features

- Full `storage.Conn` implementation (unifying DML Compilation and Execution).
- Transaction support via `BeginTx` returning `storage.TxBoundExecutor`.
- Secure SQL generation with parameterized queries using `$1`, `$2`, etc.
- Support for `Create`, `ReadOne`, `ReadAll`, `Update`, `Delete` DML actions.
- Full DDL schema creation and compilation using `ddl.Compiler`.
- High-efficiency row scanning with `sql.ErrNoRows` cleanly mapped to `storage.ErrNoRows`.
- Complies with `storage/conformance` and `ddl/conformance` test suites.

## Documentation

- [Postgres Setup & Troubleshooting](docs/POSTGRES_SETUP.md)
