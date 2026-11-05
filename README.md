# PostgreSQL Adapter for webtyp/storage + ddl
<img src="docs/img/badges.svg">

This repository implements the `storage.Conn` (from `webtyp.com/storage`) and `ddl.Compiler` (from `webtyp.com/ddl`) interfaces for PostgreSQL.

## Why This Exists

This package allows using a unified database model across different layers without writing custom SQL statements for Postgres.

| Without | With |
|---|---|
| Hand-written PostgreSQL per migration | `ddl.Sync`/`CopyColumn`/`DropColumn` and `storage` queries over one `postgres.Open` connection |

See [ARCHITECTURE](docs/ARCHITECTURE.md) and [POSTGRES_SETUP](docs/POSTGRES_SETUP.md) for more details.

## Before You Start

You need a PostgreSQL server and its DSN (Data Source Name). For instance:
`postgres://user:password@localhost:5432/dbname?sslmode=disable`

## Step-by-Step Guide

```go
package main

import (
	"log"

	"webtyp.com/ddl"
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

	// DDL sync example:
	ddl.New(conn, conn.(ddl.Compiler)).Sync(&Model{})
	// Copy and Drop column example
	// ddl.New(conn, conn.(ddl.Compiler)).CopyColumn(&Model{}, "old_column", "new_column")
	// ddl.New(conn, conn.(ddl.Compiler)).DropColumn(&Model{}, "old_column")
}
```

### Reading a backup
```go
	snap, err := postgres.Backups(dsn).OpenBackup("migrate/pastdb/1793656810107289500-db.backup")
	if err != nil { return err }
	defer snap.Close()
	rows, err := snap.ReadTable("staff")
```

Needs `pg_restore` in PATH and a URL DSN with permission to `CREATE DATABASE`. The PostgreSQL client tools must not be newer than the server (ErrPgRestoreNewer says which to install).

## Documentation Map

| File | Answers |
|---|---|
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | How the package components are structured |
| [`docs/POSTGRES_SETUP.md`](docs/POSTGRES_SETUP.md) | Postgres Setup & Troubleshooting |
| [`docs/PLAN.md`](docs/PLAN.md) | The delivery plan of this repository |
