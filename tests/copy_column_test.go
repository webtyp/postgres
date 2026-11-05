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
