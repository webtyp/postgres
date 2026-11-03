# Architecture

## Purpose
The postgres adapter provides `storage.Conn` and `ddl.Compiler` implementations for PostgreSQL.
It now also supports opening a pg_dump archive as a read-only `ddl.Snapshot` via `postgres.Backups`.

## Components
- **PostgresAdapter**: The core adapter implementing `storage.Conn` and `ddl.Compiler`.
- **Backups**: A factory for opening pg_dump archives (`ddl.BackupFormat`).
- **snapshot**: A restored archive answering `ddl.Snapshot`. It wraps `PostgresAdapter` to reuse schema introspection (Tables, Columns) and cast all column values to text during ReadTable.

## Flows
- **OpenBackup**: When opening a backup, the adapter creates a temporary database (`snapshot_<nano>`), restores the archive into it using `pg_restore` (with `--no-owner --no-acl`), connects to the restored database, and returns a snapshot.
- **Close**: The snapshot closes the connection and forcefully drops the temporary database.

## Decisions
- **restore then read with SQL; never parse the custom format**: To read a `pg_dump -Fc` archive, we leverage `pg_restore` to dump it into a temporary database rather than trying to parse the complex binary custom format directly. This is the documented and reliable way to read custom format archives.
