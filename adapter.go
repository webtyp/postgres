package postgres

import (
	"database/sql"

	_ "github.com/lib/pq"
	"webtyp.com/ddl"
	"webtyp.com/fmt"
	"webtyp.com/model"
	"webtyp.com/storage"
)

// PostgresAdapter implements storage.Conn and ddl.Compiler for PostgreSQL.
type PostgresAdapter struct {
	db *sql.DB
}

// Open creates a new postgres connection and returns it as a storage.Conn.
// No registry, no init() — construct explicitly.
func Open(dataSourceName string) (storage.Conn, error) {
	raw, err := sql.Open("postgres", dataSourceName)
	if err != nil {
		return nil, fmt.Errf("failed to open postgres connection: %v", err)
	}
	if err := raw.Ping(); err != nil {
		return nil, fmt.Errf("failed to ping postgres: %v", err)
	}
	return &PostgresAdapter{db: raw}, nil
}

// AdapterForTest wraps an already-open *sql.DB, skipping sql.Open — used by conformance tests
// that manage the connection lifecycle themselves.
func AdapterForTest(raw *sql.DB) *PostgresAdapter {
	return &PostgresAdapter{db: raw}
}

func (p *PostgresAdapter) Compile(q storage.Query, m model.Model) (storage.Plan, error) {
	sqlStr, args, err := translate(q, m)
	if err != nil {
		return storage.Plan{}, err
	}
	return storage.Plan{Mode: q.Action, Query: sqlStr, Args: args}, nil
}

// CompileDDL implements ddl.Compiler — PostgresAdapter satisfies both compiler contracts in
// the same type, so a single Open(dsn) result works for orm.New(conn) AND ddl.New(conn, conn).
func (p *PostgresAdapter) CompileDDL(s ddl.Stmt, m model.Model) (string, []any, error) {
	return translateDDL(s, m)
}

func (p *PostgresAdapter) Exec(query string, args ...any) error {
	_, err := p.db.Exec(query, args...)
	return err
}

func (p *PostgresAdapter) QueryRow(query string, args ...any) storage.Scanner {
	return &errScanner{s: p.db.QueryRow(query, args...)}
}

func (p *PostgresAdapter) Query(query string, args ...any) (storage.Rows, error) {
	rows, err := p.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	return nullRows{rows}, nil
}

func (p *PostgresAdapter) Close() error {
	return p.db.Close()
}

func (p *PostgresAdapter) TableColumns(table string) ([]string, error) {
	return tableColumns(p, table)
}

type errScanner struct{ s *sql.Row }

func (e errScanner) Scan(dest ...any) error {
	err := e.s.Scan(storage.NullSafe(dest)...)
	if err == sql.ErrNoRows {
		return storage.ErrNoRows
	}
	return err
}

// nullRows applies the storage contract's NULL rule to the read-all path.
// *sql.Rows satisfies storage.Rows on its own, which is precisely why this
// wrapper is easy to forget: without it, ReadAll answers differently from
// ReadOne on the same column.
type nullRows struct{ *sql.Rows }

func (r nullRows) Scan(dest ...any) error { return r.Rows.Scan(storage.NullSafe(dest)...) }

func tableColumns(q interface {
	Query(string, ...any) (storage.Rows, error)
}, table string) ([]string, error) {
	rows, err := q.Query(
		`SELECT column_name FROM information_schema.columns WHERE table_name = $1`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

var (
	_ storage.Conn          = (*PostgresAdapter)(nil)
	_ ddl.Compiler          = (*PostgresAdapter)(nil)
	_ ddl.TableIntrospector = (*PostgresAdapter)(nil)
)
