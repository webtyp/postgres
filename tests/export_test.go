//go:build !wasm

package tests

import "webtyp.com/model"

import (
	"os"
	"strings"
	"testing"

	"webtyp.com/postgres"
)

type userModel struct{}

func (m *userModel) ModelName() string { return "users" }
func (m *userModel) Schema() []model.Field {
	return []model.Field{
		{Name: "id", Type: model.Int(), DB: &model.FieldDB{PK: true, AutoInc: true}},
		{Name: "username", Type: model.Text(), NotNull: true, Permitted: model.Permitted{Maximum: 50}, DB: &model.FieldDB{Unique: true}},
		{Name: "email", Type: model.Text(), NotNull: true, DB: &model.FieldDB{Unique: true}},
		{Name: "score", Type: model.Float()},
		{Name: "active", Type: model.Bool()},
		{Name: "avatar", Type: model.Blob()},
	}
}
func (m *userModel) Pointers() []any                  { return nil }
func (m *userModel) EncodeFields(w model.FieldWriter) {}
func (m *userModel) DecodeFields(r model.FieldReader) {}
func (m *userModel) IsNil() bool                      { return m == nil }

type roleModel struct{}

func (m *roleModel) ModelName() string { return "roles" }
func (m *roleModel) Schema() []model.Field {
	return []model.Field{
		{Name: "id", Type: model.Int(), DB: &model.FieldDB{PK: true, AutoInc: true}},
		{Name: "name", Type: model.Text(), NotNull: true, Permitted: model.Permitted{Maximum: 100}, DB: &model.FieldDB{Unique: true}},
	}
}
func (m *roleModel) Pointers() []any                  { return nil }
func (m *roleModel) EncodeFields(w model.FieldWriter) {}
func (m *roleModel) DecodeFields(r model.FieldReader) {}
func (m *roleModel) IsNil() bool                      { return m == nil }

type sessionModel struct{}

func (m *sessionModel) ModelName() string { return "sessions" }
func (m *sessionModel) Schema() []model.Field {
	return []model.Field{
		{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "user_id", Type: model.Int()},
		{Name: "metadata", Type: model.Text()},
	}
}
func (m *sessionModel) SchemaExt() []model.FieldExt {
	return []model.FieldExt{
		{Field: model.Field{Name: "user_id"}, Ref: "users"},
	}
}
func (m *sessionModel) Pointers() []any                  { return nil }
func (m *sessionModel) EncodeFields(w model.FieldWriter) {}
func (m *sessionModel) DecodeFields(r model.FieldReader) {}
func (m *sessionModel) IsNil() bool                      { return m == nil }

type userRoleModel struct{}

func (m *userRoleModel) ModelName() string { return "user_roles" }
func (m *userRoleModel) Schema() []model.Field {
	return []model.Field{
		{Name: "user_id", Type: model.Int(), DB: &model.FieldDB{PK: true}},
		{Name: "role_id", Type: model.Int(), DB: &model.FieldDB{PK: true}},
	}
}
func (m *userRoleModel) SchemaExt() []model.FieldExt {
	return []model.FieldExt{
		{Field: model.Field{Name: "user_id"}, Ref: "users"},
		{Field: model.Field{Name: "role_id"}, Ref: "roles"},
	}
}
func (m *userRoleModel) Pointers() []any                  { return nil }
func (m *userRoleModel) EncodeFields(w model.FieldWriter) {}
func (m *userRoleModel) DecodeFields(r model.FieldReader) {}
func (m *userRoleModel) IsNil() bool                      { return m == nil }

func TestExportDDL_FullSchema(t *testing.T) {
	goldenRaw, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatalf("failed to read golden file: %v", err)
	}

	// Remove header comments from golden file for comparison
	lines := strings.Split(string(goldenRaw), "\n")
	var sb strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "CREATE") || strings.HasPrefix(line, "-- dialect:") {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	golden := sb.String()

	c := postgres.NewCompiler()
	models := []model.Model{
		&userModel{},
		&roleModel{},
		&sessionModel{},
		&userRoleModel{},
	}

	got, err := c.ExportDDL(models)
	if err != nil {
		t.Fatalf("ExportDDL failed: %v", err)
	}

	gotClean := ""
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "CREATE") || strings.HasPrefix(line, "-- dialect:") {
			gotClean += line + "\n"
		}
	}

	if strings.TrimSpace(gotClean) != strings.TrimSpace(golden) {
		t.Errorf("DDL output mismatch.\nGOT:\n%s\n\nEXPECTED:\n%s", gotClean, golden)
	}
}

func TestExportDDL_EmptyInput(t *testing.T) {
	c := postgres.NewCompiler()
	got, err := c.ExportDDL(nil)
	if err != nil {
		t.Fatalf("ExportDDL failed: %v", err)
	}
	expected := "-- dialect: postgres\n\n"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestExportDDL_ImplementsInterface(t *testing.T) {
	var _ interface {
		ExportDDL(models []model.Model) (string, error)
	} = postgres.NewCompiler()
}
