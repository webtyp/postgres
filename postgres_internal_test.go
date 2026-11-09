package postgres

import (
	"database/sql"
	"os"
	"testing"

	"webtyp.com/ddl"
	"webtyp.com/fmt"
	"webtyp.com/model"
	"webtyp.com/storage"
)

type mockInternalModel struct{}

func (m *mockInternalModel) ModelName() string { return "internal_test" }
func (m *mockInternalModel) Schema() []model.Field {
	return []model.Field{
		{Name: "id", Type: model.Int(), DB: &model.FieldDB{PK: true, AutoInc: true}},
		{Name: "name", Type: model.Text(), NotNull: true},
	}
}
func (m *mockInternalModel) Pointers() []any                  { return nil }
func (m *mockInternalModel) EncodeFields(w model.FieldWriter) {}
func (m *mockInternalModel) DecodeFields(r model.FieldReader) {}
func (m *mockInternalModel) IsNil() bool                      { return m == nil }

type compositePKModel struct{}

func (m *compositePKModel) ModelName() string { return "composite_pk" }
func (m *compositePKModel) Schema() []model.Field {
	return []model.Field{
		{Name: "id1", Type: model.Int(), DB: &model.FieldDB{PK: true}},
		{Name: "id2", Type: model.Int(), DB: &model.FieldDB{PK: true}},
	}
}
func (m *compositePKModel) Pointers() []any                  { return nil }
func (m *compositePKModel) EncodeFields(w model.FieldWriter) {}
func (m *compositePKModel) DecodeFields(r model.FieldReader) {}
func (m *compositePKModel) IsNil() bool                      { return m == nil }

type cyclicModelA struct{}

func (m *cyclicModelA) ModelName() string { return "cyclic_a" }
func (m *cyclicModelA) Schema() []model.Field {
	return []model.Field{{Name: "id", Type: model.Int(), DB: &model.FieldDB{PK: true}}}
}
func (m *cyclicModelA) SchemaExt() []model.FieldExt {
	return []model.FieldExt{{Field: model.Field{Name: "b_id"}, Ref: "cyclic_b"}}
}
func (m *cyclicModelA) Pointers() []any                  { return nil }
func (m *cyclicModelA) EncodeFields(w model.FieldWriter) {}
func (m *cyclicModelA) DecodeFields(r model.FieldReader) {}
func (m *cyclicModelA) IsNil() bool                      { return m == nil }

type cyclicModelB struct{}

func (m *cyclicModelB) ModelName() string { return "cyclic_b" }
func (m *cyclicModelB) Schema() []model.Field {
	return []model.Field{{Name: "id", Type: model.Int(), DB: &model.FieldDB{PK: true}}}
}
func (m *cyclicModelB) SchemaExt() []model.FieldExt {
	return []model.FieldExt{{Field: model.Field{Name: "a_id"}, Ref: "cyclic_a"}}
}
func (m *cyclicModelB) Pointers() []any                  { return nil }
func (m *cyclicModelB) EncodeFields(w model.FieldWriter) {}
func (m *cyclicModelB) DecodeFields(r model.FieldReader) {}
func (m *cyclicModelB) IsNil() bool                      { return m == nil }

type autoIncNonPKModel struct{}

func (m *autoIncNonPKModel) ModelName() string { return "autoinc_non_pk" }
func (m *autoIncNonPKModel) Schema() []model.Field {
	return []model.Field{
		{Name: "id", Type: model.Int(), DB: &model.FieldDB{PK: true}},
		{Name: "seq", Type: model.Int(), DB: &model.FieldDB{AutoInc: true}},
		{Name: "uid", Type: model.Text(), DB: &model.FieldDB{Unique: true}},
	}
}
func (m *autoIncNonPKModel) Pointers() []any                  { return nil }
func (m *autoIncNonPKModel) EncodeFields(w model.FieldWriter) {}
func (m *autoIncNonPKModel) DecodeFields(r model.FieldReader) {}
func (m *autoIncNonPKModel) IsNil() bool                      { return m == nil }

type allTypesModel struct{}

func (m *allTypesModel) ModelName() string { return "all_types" }
func (m *allTypesModel) Schema() []model.Field {
	return []model.Field{
		{Name: "f_int", Type: model.Int()},
		{Name: "f_float", Type: model.Float()},
		{Name: "f_bool", Type: model.Bool()},
		{Name: "f_blob", Type: model.Blob()},
	}
}
func (m *allTypesModel) Pointers() []any                  { return nil }
func (m *allTypesModel) EncodeFields(w model.FieldWriter) {}
func (m *allTypesModel) DecodeFields(r model.FieldReader) {}
func (m *allTypesModel) IsNil() bool                      { return m == nil }

// reservedWordModel mirrors webtyp/user's real UserModel: a table literally
// named "user" — a Postgres reserved keyword. This is the exact shape that
// produced "pq: syntax error at or near "user" at column 28" before
// quoteIdent existed.
type reservedWordModel struct{}

func (m *reservedWordModel) ModelName() string { return "user" }
func (m *reservedWordModel) Schema() []model.Field {
	return []model.Field{
		{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "order", Type: model.Text()}, // also reserved — column position, not just table
	}
}
func (m *reservedWordModel) Pointers() []any                  { return nil }
func (m *reservedWordModel) EncodeFields(w model.FieldWriter) {}
func (m *reservedWordModel) DecodeFields(r model.FieldReader) {}
func (m *reservedWordModel) IsNil() bool                      { return m == nil }

func TestQuoteIdent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"*", "*"},
		{"user", `"user"`},
		{"id", `"id"`},
		{`we"ird`, `"we""ird"`},
		{"t.col", `"t"."col"`},
	}
	for _, c := range cases {
		if got := quoteIdent(c.in); got != c.want {
			t.Errorf("quoteIdent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestTranslateDDL_ReservedWordTable is the regression net for the "user"
// syntax error: every identifier translateDDL writes for a reserved-word
// table/column must come out quoted, never bare.
func TestTranslateDDL_ReservedWordTable(t *testing.T) {
	query, _, err := translateDDL(ddl.Stmt{Op: ddl.OpCreateTable, Table: "user"}, &reservedWordModel{})
	if err != nil {
		t.Fatalf("translateDDL failed: %v", err)
	}
	want := `CREATE TABLE IF NOT EXISTS "user" ("id" TEXT PRIMARY KEY, "order" TEXT)`
	if query != want {
		t.Errorf("got:\n%s\nwant:\n%s", query, want)
	}

	dropQuery, _, err := translateDDL(ddl.Stmt{Op: ddl.OpDropTable, Table: "user"}, &reservedWordModel{})
	if err != nil {
		t.Fatalf("translateDDL drop failed: %v", err)
	}
	if dropQuery != `DROP TABLE IF EXISTS "user"` {
		t.Errorf("got %q", dropQuery)
	}
}

// TestTranslate_ReservedWordTable covers the DML side (INSERT/SELECT/UPDATE/
// DELETE, plus WHERE/ORDER BY) against the same "user" table and an "order"
// column, so every identifier-writing branch in translate/buildConditions is
// exercised at least once.
func TestTranslate_ReservedWordTable(t *testing.T) {
	insert, _, err := translate(storage.Query{
		Action: storage.ActionCreate, Table: "user", Columns: []string{"order"}, Values: []any{"x"},
	}, &reservedWordModel{})
	if err != nil || insert != `INSERT INTO "user" ("order") VALUES ($1)` {
		t.Errorf("insert: got %q, err %v", insert, err)
	}

	sel, _, err := translate(storage.Query{
		Action: storage.ActionReadAll, Table: "user", Columns: []string{"order"},
		Conditions: []storage.Condition{storage.Eq("order", "x")},
		OrderBy:    []storage.Order{storage.Asc("order")},
	}, &reservedWordModel{})
	want := `SELECT "order" FROM "user" WHERE "order" = $1 ORDER BY "order" ASC`
	if err != nil || sel != want {
		t.Errorf("select: got %q, want %q, err %v", sel, want, err)
	}

	upd, _, err := translate(storage.Query{
		Action: storage.ActionUpdate, Table: "user", Columns: []string{"order"}, Values: []any{"y"},
	}, &reservedWordModel{})
	if err != nil || upd != `UPDATE "user" SET "order" = $1` {
		t.Errorf("update: got %q, err %v", upd, err)
	}

	del, _, err := translate(storage.Query{Action: storage.ActionDelete, Table: "user"}, &reservedWordModel{})
	if err != nil || del != `DELETE FROM "user"` {
		t.Errorf("delete: got %q, err %v", del, err)
	}
}

type errorQuerier struct{}

func (e errorQuerier) Query(query string, args ...any) (storage.Rows, error) {
	return nil, fmt.Err("mock query error")
}

func TestInternal_Coverage(t *testing.T) {
	// Test helper functions in translate.go
	if onDeleteSQL("restrict") != "RESTRICT" {
		t.Error("expected RESTRICT")
	}
	if onDeleteSQL("set_null") != "SET NULL" {
		t.Error("expected SET NULL")
	}
	if onDeleteSQL("no_action") != "NO ACTION" {
		t.Error("expected NO ACTION")
	}
	if onDeleteSQL("cascade") != "CASCADE" {
		t.Error("expected CASCADE")
	}

	// Test compile errors and invalid inputs
	invalidQuery := storage.Query{Action: storage.Action(999)}
	_, _, err := translate(invalidQuery, &mockInternalModel{})
	if err == nil {
		t.Error("expected error on invalid DML action")
	}

	invalidStmt := ddl.Stmt{Op: ddl.Op(999)}
	_, _, err = translateDDL(invalidStmt, &mockInternalModel{})
	if err == nil {
		t.Error("expected error on invalid DDL op")
	}

	// DDL: OpAddColumn error
	_, _, err = translateDDL(ddl.Stmt{Op: ddl.OpAddColumn}, &mockInternalModel{})
	if err == nil {
		t.Error("expected error on OpAddColumn with nil column")
	}
	_, _, err = translateDDL(ddl.Stmt{Op: ddl.OpAddColumn, Column: &model.Field{Name: "bio", Type: model.Text()}}, &mockInternalModel{})
	if err == nil {
		t.Error("expected error on OpAddColumn with empty Table")
	}

	// DDL: OpRenameColumn error
	_, _, err = translateDDL(ddl.Stmt{Op: ddl.OpRenameColumn}, &mockInternalModel{})
	if err == nil {
		t.Error("expected error on OpRenameColumn with nil column")
	}
	_, _, err = translateDDL(ddl.Stmt{Op: ddl.OpRenameColumn, Column: &model.Field{Name: "bio", Type: model.Text()}, OldName: "old"}, &mockInternalModel{})
	if err == nil {
		t.Error("expected error on OpRenameColumn with empty Table")
	}

	// DDL: OpDropColumn error
	_, _, err = translateDDL(ddl.Stmt{Op: ddl.OpDropColumn}, &mockInternalModel{})
	if err == nil {
		t.Error("expected error on OpDropColumn with empty column name")
	}
	_, _, err = translateDDL(ddl.Stmt{Op: ddl.OpDropColumn, ColumnName: "bio"}, &mockInternalModel{})
	if err == nil {
		t.Error("expected error on OpDropColumn with empty Table")
	}

	// Test public exports Translate & TranslateDDL
	_, _, _ = Translate(storage.Query{Action: storage.ActionCreate, Table: "t"}, &mockInternalModel{})
	_, _, _ = TranslateDDL(ddl.Stmt{Op: ddl.OpDropTable, Table: "t"}, &mockInternalModel{})

	// Test compile error inside adapter Compile
	adapterNilDB := &PostgresAdapter{}
	_, err = adapterNilDB.Compile(invalidQuery, &mockInternalModel{})
	if err == nil {
		t.Error("expected Compile to propagate error from translate")
	}

	// Test composite PK in translateDDL
	_, _, err = translateDDL(ddl.Stmt{Op: ddl.OpCreateTable, Table: "composite_pk"}, &compositePKModel{})
	if err != nil {
		t.Errorf("expected composite PK translation to succeed, got: %v", err)
	}

	// Test autoIncNonPKModel in translateDDL
	_, _, err = translateDDL(ddl.Stmt{Op: ddl.OpCreateTable, Table: "autoinc_non_pk"}, &autoIncNonPKModel{})
	if err != nil {
		t.Errorf("expected autoIncNonPKModel translation to succeed, got: %v", err)
	}

	// Test allTypesModel in translateDDL
	_, _, err = translateDDL(ddl.Stmt{Op: ddl.OpCreateTable, Table: "all_types"}, &allTypesModel{})
	if err != nil {
		t.Errorf("expected allTypesModel translation to succeed, got: %v", err)
	}

	// Test topological sort error in ExportDDL
	comp := NewCompiler()
	_, err = comp.ExportDDL([]model.Model{&cyclicModelA{}, &cyclicModelB{}})
	if err == nil {
		t.Error("expected ExportDDL to fail on cyclic dependencies")
	}

	// Test error paths in tableColumns, tables, and columns functions
	eq := errorQuerier{}
	_, err = tableColumns(eq, "t")
	if err == nil {
		t.Error("expected tableColumns to fail with query error")
	}
	_, err = tables(eq)
	if err == nil {
		t.Error("expected tables to fail with query error")
	}
	_, err = columns(eq, "t")
	if err == nil {
		t.Error("expected columns to fail with query error")
	}

	// Test IN conditions corner cases
	sb := buildConditionsTest()
	var args []any
	argIndex := 1
	err = buildConditions(sb, []storage.Condition{
		storage.In("id", 123), // non-slice
	}, &args, &argIndex)
	if err == nil {
		t.Error("expected error for non-slice IN value")
	}

	err = buildConditions(sb, []storage.Condition{
		storage.In("id", []any{}), // empty slice
	}, &args, &argIndex)
	if err == nil {
		t.Error("expected error for empty slice IN value")
	}

	// Test standard operators and conditional logic
	sb = buildConditionsTest()
	args = nil
	argIndex = 1
	err = buildConditions(sb, []storage.Condition{
		storage.Eq("name", "Alice"),
		storage.Or(storage.Eq("name", "Bob")),
		storage.IsNotNull("active"),
	}, &args, &argIndex)
	if err != nil {
		t.Errorf("failed to build conditions: %v", err)
	}

	// Test Translate with orderBy, limit, and offset
	readQuery := storage.Query{
		Action:  storage.ActionReadAll,
		Table:   "internal_test",
		Columns: []string{"id", "name"},
		OrderBy: []storage.Order{
			storage.Asc("name"),
			storage.Desc("id"),
		},
		Limit:  10,
		Offset: 2,
	}
	_, _, err = translate(readQuery, &mockInternalModel{})
	if err != nil {
		t.Errorf("failed to translate ReadAll with order, limit, offset: %v", err)
	}

	// Connection tests
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set, skipping integration coverage")
	}

	// Test Open error
	_, err = Open("postgres://invalid:password@localhost:5432/nonexistent?sslmode=disable")
	if err == nil {
		t.Error("expected failure to open invalid dsn connection")
	}

	conn, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer conn.Close()

	adapter := conn.(*PostgresAdapter)

	// Test BeginTx error with closed connection
	rawClose, err := sql.Open("postgres", dsn)
	if err == nil {
		rawClose.Close()
		adapterClosed := AdapterForTest(rawClose)
		_, err = adapterClosed.BeginTx()
		if err == nil {
			t.Error("expected BeginTx to fail on closed raw connection")
		}
	}

	// Clean up table if it exists
	_, _ = adapter.Exec("DROP TABLE IF EXISTS internal_test CASCADE")

	// Create table using adapter CompileDDL
	createStmt := ddl.Stmt{Op: ddl.OpCreateTable, Table: "internal_test"}
	query, _, err := adapter.CompileDDL(createStmt, &mockInternalModel{})
	if err != nil {
		t.Fatalf("CompileDDL failed: %v", err)
	}
	if _, err := adapter.Exec(query); err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Test Introspection (Tables and Columns)
	tbls, err := adapter.Tables()
	if err != nil {
		t.Fatalf("Tables failed: %v", err)
	}
	found := false
	for _, tbl := range tbls {
		if tbl == "internal_test" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'internal_test' table to be found in Tables()")
	}

	cols, err := adapter.Columns("internal_test")
	if err != nil {
		t.Fatalf("Columns failed: %v", err)
	}
	if len(cols) != 2 {
		t.Errorf("expected 2 columns, got %d", len(cols))
	}

	// Test Compile DML
	insertQuery := storage.Query{
		Action:  storage.ActionCreate,
		Table:   "internal_test",
		Columns: []string{"name"},
		Values:  []any{"hello"},
	}
	plan, err := adapter.Compile(insertQuery, &mockInternalModel{})
	if err != nil {
		t.Fatalf("Compile insert failed: %v", err)
	}

	// Test Exec / Query / QueryRow
	if _, err := adapter.Exec(plan.Query, plan.Args...); err != nil {
		t.Fatalf("Exec insert failed: %v", err)
	}

	var name string
	err = adapter.QueryRow("SELECT name FROM internal_test WHERE id = $1", 1).Scan(&name)
	if err != nil {
		t.Fatalf("QueryRow scan failed: %v", err)
	}
	if name != "hello" {
		t.Errorf("expected 'hello', got %q", name)
	}

	// errScanner ErrNoRows
	err = adapter.QueryRow("SELECT name FROM internal_test WHERE id = $1", 999).Scan(&name)
	if err != storage.ErrNoRows {
		t.Errorf("expected storage.ErrNoRows, got %v", err)
	}

	// Test PostgresTx Coverage
	txBound, err := adapter.BeginTx()
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer txBound.Close()

	tx := txBound.(*PostgresTx)

	// tx compile
	_, err = tx.Compile(insertQuery, &mockInternalModel{})
	if err != nil {
		t.Errorf("tx.Compile failed: %v", err)
	}

	// tx CompileDDL
	_, _, err = tx.CompileDDL(ddl.Stmt{Op: ddl.OpDropTable, Table: "internal_test"}, &mockInternalModel{})
	if err != nil {
		t.Errorf("tx.CompileDDL failed: %v", err)
	}

	// tx Exec / Query / QueryRow
	_, err = tx.Exec("INSERT INTO internal_test (name) VALUES ($1)", "tx-test")
	if err != nil {
		t.Errorf("tx.Exec failed: %v", err)
	}

	err = tx.QueryRow("SELECT name FROM internal_test WHERE name = $1", "tx-test").Scan(&name)
	if err != nil {
		t.Errorf("tx.QueryRow failed: %v", err)
	}

	rows, err := tx.Query("SELECT name FROM internal_test")
	if err != nil {
		t.Errorf("tx.Query failed: %v", err)
	}
	rows.Close()

	// tx Introspection
	_, err = tx.Tables()
	if err != nil {
		t.Errorf("tx.Tables failed: %v", err)
	}
	_, err = tx.Columns("internal_test")
	if err != nil {
		t.Errorf("tx.Columns failed: %v", err)
	}
	_, err = tx.TableColumns("internal_test")
	if err != nil {
		t.Errorf("tx.TableColumns failed: %v", err)
	}

	// tx Rollback
	err = tx.Rollback()
	if err != nil {
		t.Errorf("tx.Rollback failed: %v", err)
	}
}

func buildConditionsTest() *fmt.Conv {
	return fmt.Convert()
}
