package postgres

import (
	"context"
	"database/sql"

	"webtyp.com/ddl"
	"webtyp.com/model"
	"webtyp.com/storage"
)

// PostgresTx wraps a SQL transaction.
type PostgresTx struct {
	tx      *sql.Tx
	adapter *PostgresAdapter
}

// Ensure PostgresTx implements ddl.Compiler.
var _ ddl.Compiler = (*PostgresTx)(nil)

// Ensure PostgresTx implements storage.Conn.
var _ storage.Conn = (*PostgresTx)(nil)

// Compile delegates to the PostgresAdapter.
func (p *PostgresTx) Compile(q storage.Query, m model.Model) (storage.Plan, error) {
	return p.adapter.Compile(q, m)
}

// CompileDDL delegates to translateDDL.
func (p *PostgresTx) CompileDDL(s ddl.Stmt, m model.Model) (string, []any, error) {
	return translateDDL(s, m)
}

// Exec executes a query within the transaction.
func (p *PostgresTx) Exec(query string, args ...any) error {
	_, err := p.tx.Exec(query, args...)
	return err
}

// QueryRow executes a query that is expected to return at most one row within the transaction.
func (p *PostgresTx) QueryRow(query string, args ...any) storage.Scanner {
	return &errScanner{s: p.tx.QueryRow(query, args...)}
}

// Query executes a query that returns rows within the transaction.
func (p *PostgresTx) Query(query string, args ...any) (storage.Rows, error) {
	rows, err := p.tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	return nullRows{rows}, nil
}

// Close is a no-op for storage.Conn in this context.
func (p *PostgresTx) Close() error {
	return nil
}

// Commit commits the transaction.
func (p *PostgresTx) Commit() error {
	return p.tx.Commit()
}

// Rollback aborts the transaction.
func (p *PostgresTx) Rollback() error {
	return p.tx.Rollback()
}

// TableColumns returns the column names for the given table within the transaction.
func (p *PostgresTx) TableColumns(table string) ([]string, error) {
	return tableColumns(p, table)
}

// Tables returns all user-defined table names in the current schema.
func (p *PostgresTx) Tables() ([]string, error) {
	return tables(p)
}

// Columns returns full column metadata for the given table.
func (p *PostgresTx) Columns(table string) ([]ddl.ColumnInfo, error) {
	return columns(p, table)
}

// Ensure PostgresTx implements ddl.SchemaInspector.
var _ ddl.SchemaInspector = (*PostgresTx)(nil)

// BeginTx starts a transaction and returns a new storage.TxBoundExecutor.
func (p *PostgresAdapter) BeginTx() (storage.TxBoundExecutor, error) {
	tx, err := p.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	return &PostgresTx{tx: tx, adapter: p}, nil
}
