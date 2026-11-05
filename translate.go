package postgres

import (
	"strings"

	"webtyp.com/ddl"
	"webtyp.com/fmt"
	"webtyp.com/model"
	"webtyp.com/storage"
)

// quoteIdent double-quotes a Postgres identifier so a table or column named
// after a reserved word (user, order, group, ...) survives as a literal
// identifier instead of being parsed as the keyword — an unquoted `user`
// table produced "syntax error at or near "user"" here. Embedded double
// quotes are doubled, per the SQL standard; a dotted name is quoted part by
// part rather than as one literal, in case a caller ever passes a qualified
// reference.
func quoteIdent(name string) string {
	if name == "" || name == "*" {
		return name
	}
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return quoteIdent(name[:i]) + "." + quoteIdent(name[i+1:])
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteIdentList(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = quoteIdent(n)
	}
	return out
}

func postgresType(t model.FieldType) string {
	switch t {
	case model.FieldInt:
		return "BIGINT"
	case model.FieldFloat:
		return "DOUBLE PRECISION"
	case model.FieldBool:
		return "BOOLEAN"
	case model.FieldBlob:
		return "BYTEA"
	default:
		return "TEXT"
	}
}

func postgresColumnType(f model.Field) string {
	if f.Type.Storage() == model.FieldText && f.Permitted.Maximum > 0 {
		return fmt.Sprintf("VARCHAR(%d)", f.Permitted.Maximum)
	}
	return postgresType(f.Type.Storage())
}

func onDeleteSQL(action string) string {
	switch action {
	case "restrict":
		return "RESTRICT"
	case "set_null":
		return "SET NULL"
	case "no_action":
		return "NO ACTION"
	default:
		return "CASCADE"
	}
}

// translate converts a storage.Query (DML only) into Postgres SQL.
func translate(q storage.Query, m model.Model) (string, []any, error) {
	sb := fmt.Convert()
	var args []any
	argIndex := 1

	switch q.Action {
	case storage.ActionCreate:
		sb.Write("INSERT INTO ")
		sb.Write(quoteIdent(q.Table))
		sb.Write(" (")
		sb.Write(fmt.Convert(quoteIdentList(q.Columns)).Join(", ").String())
		sb.Write(") VALUES (")
		for i, v := range q.Values {
			if i > 0 {
				sb.Write(", ")
			}
			sb.Write(fmt.Sprintf("$%d", argIndex))
			args = append(args, v)
			argIndex++
		}
		sb.Write(")")

	case storage.ActionReadOne, storage.ActionReadAll:
		sb.Write("SELECT ")
		if len(q.Columns) == 0 {
			sb.Write("*")
		} else {
			sb.Write(fmt.Convert(quoteIdentList(q.Columns)).Join(", ").String())
		}
		sb.Write(" FROM ")
		sb.Write(quoteIdent(q.Table))
		if err := buildConditions(sb, q.Conditions, &args, &argIndex); err != nil {
			return "", nil, err
		}
		if len(q.OrderBy) > 0 {
			sb.Write(" ORDER BY ")
			for i, o := range q.OrderBy {
				if i > 0 {
					sb.Write(", ")
				}
				sb.Write(quoteIdent(o.Column()))
				sb.Write(" ")
				sb.Write(o.Dir())
			}
		}
		if q.Limit > 0 {
			sb.Write(fmt.Sprintf(" LIMIT %d", q.Limit))
		}
		if q.Offset > 0 {
			sb.Write(fmt.Sprintf(" OFFSET %d", q.Offset))
		}

	case storage.ActionUpdate:
		sb.Write("UPDATE ")
		sb.Write(quoteIdent(q.Table))
		sb.Write(" SET ")

		isPKCol := make(map[string]bool)
		if m != nil {
			for _, f := range m.Schema() {
				if f.IsPK() || f.IsAutoInc() {
					isPKCol[f.Name] = true
				}
			}
		}

		added := 0
		for i, c := range q.Columns {
			if isPKCol[c] {
				continue
			}
			if added > 0 {
				sb.Write(", ")
			}
			sb.Write(quoteIdent(c))
			sb.Write(fmt.Sprintf(" = $%d", argIndex))
			args = append(args, q.Values[i])
			argIndex++
			added++
		}

		// Fallback to original behavior if no columns are left (should not happen for valid updates)
		if added == 0 {
			for i, c := range q.Columns {
				if i > 0 {
					sb.Write(", ")
				}
				sb.Write(quoteIdent(c))
				sb.Write(fmt.Sprintf(" = $%d", argIndex))
				args = append(args, q.Values[i])
				argIndex++
			}
		}

		if err := buildConditions(sb, q.Conditions, &args, &argIndex); err != nil {
			return "", nil, err
		}

	case storage.ActionDelete:
		sb.Write("DELETE FROM ")
		sb.Write(quoteIdent(q.Table))
		if err := buildConditions(sb, q.Conditions, &args, &argIndex); err != nil {
			return "", nil, err
		}

	default:
		return "", nil, fmt.Errf("postgres: unknown DML action: %v", q.Action)
	}

	return sb.String(), args, nil
}

const errNameMissing = "postgres: a table or column name is missing in"

var (
	stmtAddColumn    = "ADD COLUMN"
	stmtRenameColumn = "RENAME COLUMN"
	stmtDropColumn   = "DROP COLUMN"
	stmtCopyColumn   = "UPDATE (copy column)"
)

// translateDDL converts a ddl.Stmt into Postgres SQL.
func translateDDL(s ddl.Stmt, m model.Model) (string, []any, error) {
	sb := fmt.Convert()

	switch s.Op {
	case ddl.OpCreateTable:
		sb.Write("CREATE TABLE IF NOT EXISTS ")
		sb.Write(quoteIdent(s.Table))
		sb.Write(" (")
		fields := m.Schema()

		var pkCols []string
		for _, f := range fields {
			if f.IsPK() {
				pkCols = append(pkCols, f.Name)
			}
		}
		compositePK := len(pkCols) > 1

		for i, f := range fields {
			if i > 0 {
				sb.Write(", ")
			}
			sb.Write(quoteIdent(f.Name))
			sb.Write(" ")
			isPK := f.IsPK()
			isAuto := f.IsAutoInc()
			if isPK && isAuto && !compositePK {
				if f.Type.Storage() == model.FieldInt {
					sb.Write("BIGSERIAL")
				} else {
					sb.Write("SERIAL")
				}
			} else if isAuto && f.Type.Storage() == model.FieldInt {
				sb.Write("BIGSERIAL")
			} else if isAuto {
				sb.Write("SERIAL")
			} else {
				sb.Write(postgresColumnType(f))
			}
			if isPK {
				if compositePK {
					sb.Write(" NOT NULL")
				} else {
					sb.Write(" PRIMARY KEY")
				}
			}
			if f.NotNull {
				sb.Write(" NOT NULL")
			}
			if f.IsUnique() {
				sb.Write(" UNIQUE")
			}
		}
		if compositePK {
			sb.Write(fmt.Sprintf(", PRIMARY KEY (%s)", fmt.Convert(quoteIdentList(pkCols)).Join(", ").String()))
		}
		if ext, ok := m.(interface{ SchemaExt() []model.FieldExt }); ok {
			for _, f := range ext.SchemaExt() {
				if f.Ref != "" {
					refCol := f.RefColumn
					if refCol == "" {
						refCol = "id"
					}
					sb.Write(fmt.Sprintf(", CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE %s",
						quoteIdent(fmt.Sprintf("fk_%s_%s", s.Table, f.Name)), quoteIdent(f.Name), quoteIdent(f.Ref), quoteIdent(refCol), onDeleteSQL(f.OnDelete)))
				}
			}
		}
		sb.Write(")")

	case ddl.OpDropTable:
		sb.Write("DROP TABLE IF EXISTS ")
		sb.Write(quoteIdent(s.Table))

	case ddl.OpAddColumn:
		if s.Column == nil || s.Table == "" {
			return "", nil, fmt.Err(errNameMissing, stmtAddColumn)
		}
		sb.Write("ALTER TABLE ")
		sb.Write(quoteIdent(s.Table))
		sb.Write(" ADD COLUMN IF NOT EXISTS ")
		sb.Write(quoteIdent(s.Column.Name))
		sb.Write(" ")
		sb.Write(postgresType(s.Column.Type.Storage()))

	case ddl.OpCopyColumn:
		if s.Table == "" || s.OldName == "" || s.ColumnName == "" {
			return "", nil, fmt.Err(errNameMissing, stmtCopyColumn)
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

	case ddl.OpRenameColumn:
		if s.Column == nil || s.OldName == "" || s.Table == "" {
			return "", nil, fmt.Err(errNameMissing, stmtRenameColumn)
		}
		sb.Write("ALTER TABLE ")
		sb.Write(quoteIdent(s.Table))
		sb.Write(" RENAME COLUMN ")
		sb.Write(quoteIdent(s.OldName))
		sb.Write(" TO ")
		sb.Write(quoteIdent(s.Column.Name))

	case ddl.OpDropColumn:
		if s.Table == "" || s.ColumnName == "" {
			return "", nil, fmt.Err(errNameMissing, stmtDropColumn)
		}
		sb.Write("ALTER TABLE ")
		sb.Write(quoteIdent(s.Table))
		sb.Write(" DROP COLUMN IF EXISTS ")
		sb.Write(quoteIdent(s.ColumnName))

	default:
		return "", nil, fmt.Errf("postgres: unknown DDL op: %v", s.Op)
	}

	return sb.String(), nil, nil
}

// Translate is the public DML export.
func Translate(q storage.Query, m model.Model) (string, []any, error) {
	return translate(q, m)
}

// TranslateDDL is the new DDL counterpart.
func TranslateDDL(s ddl.Stmt, m model.Model) (string, []any, error) {
	return translateDDL(s, m)
}

func buildConditions(sb *fmt.Conv, conditions []storage.Condition, args *[]any, argIndex *int) error {
	if len(conditions) == 0 {
		return nil
	}

	sb.Write(" WHERE ")
	for i, c := range conditions {
		if i > 0 {
			logic := c.Logic()
			if logic == "" {
				logic = "AND"
			}
			sb.Write(fmt.Sprintf(" %s ", logic))
		}

		op := c.Operator()
		if op == "IS NULL" || op == "IS NOT NULL" {
			sb.Write(quoteIdent(c.Field()))
			sb.Write(" ")
			sb.Write(op)
			continue
		}

		if op == "IN" {
			slice, ok := c.Value().([]any)
			if !ok {
				return fmt.Errf("IN operator requires []any value, got %T", c.Value())
			}
			if len(slice) == 0 {
				return fmt.Err("IN operator slice cannot be empty")
			}
			sb.Write(quoteIdent(c.Field()))
			sb.Write(" IN (")
			for j, val := range slice {
				if j > 0 {
					sb.Write(", ")
				}
				sb.Write(fmt.Sprintf("$%d", *argIndex))
				*args = append(*args, val)
				(*argIndex)++
			}
			sb.Write(")")
		} else {
			sb.Write(quoteIdent(c.Field()))
			sb.Write(" ")
			sb.Write(c.Operator())
			sb.Write(" ")
			sb.Write(fmt.Sprintf("$%d", *argIndex))
			*args = append(*args, c.Value())
			(*argIndex)++
		}
	}
	return nil
}
