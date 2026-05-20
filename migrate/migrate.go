// Package migrate provides DuckDB-specific schema migration helpers for ent.
//
// Generated ent packages use the PostgreSQL dialect, but DuckDB does not
// implement every PostgreSQL DDL statement ent may emit. This package keeps the
// generated table metadata and migration options, while adapting the parts
// DuckDB v1.5.x cannot run directly.
package migrate

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/lib-x/entduck"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"entgo.io/ent/schema/field"
)

var (
	// WithGlobalUniqueID sets the universal ids option for the migration.
	WithGlobalUniqueID = schema.WithGlobalUniqueID
	// WithDropColumn enables dropping obsolete columns during migration.
	WithDropColumn = schema.WithDropColumn
	// WithDropIndex enables dropping obsolete indexes during migration.
	WithDropIndex = schema.WithDropIndex
	// WithForeignKeys enables creating foreign-key constraints in DDL.
	WithForeignKeys = schema.WithForeignKeys
)

// Create runs schema creation with DuckDB-compatible table metadata.
func Create(ctx context.Context, drv dialect.Driver, tables []*schema.Table, opts ...schema.MigrateOption) error {
	tables, err := NormalizeTables(tables)
	if err != nil {
		return fmt.Errorf("entduck/migrate: normalize tables: %w", err)
	}
	created, err := createMissingTables(ctx, drv, tables)
	if err != nil {
		return err
	}
	migrateTables := filterCreatedTables(tables, created)
	if len(migrateTables) == 0 {
		return nil
	}
	opts = append([]schema.MigrateOption{schema.WithAtlas(false), schema.WithForeignKeys(false)}, opts...)
	m, err := schema.NewMigrate(drv, opts...)
	if err != nil {
		return fmt.Errorf("entduck/migrate: create schema migrator: %w", err)
	}
	if err := m.Create(ctx, migrateTables...); err != nil {
		return fmt.Errorf("entduck/migrate: create tables: %w", err)
	}
	return nil
}

// NormalizeTables copies generated ent schema tables and applies DuckDB DDL
// limitations without mutating the package-level generated Tables variable.
func NormalizeTables(tables []*schema.Table) ([]*schema.Table, error) {
	copied, err := schema.CopyTables(tables)
	if err != nil {
		return nil, err
	}
	for _, t := range copied {
		for _, c := range t.Columns {
			if c.Type == field.TypeJSON {
				if c.SchemaType == nil {
					c.SchemaType = make(map[string]string, 1)
				}
				c.SchemaType[dialect.Postgres] = "json"
			}
		}
		for _, fk := range t.ForeignKeys {
			switch fk.OnDelete {
			case schema.Cascade, schema.SetNull, schema.SetDefault:
				fk.OnDelete = schema.NoAction
			}
		}
	}
	return copied, nil
}

// Schema mirrors the generated ent migration API but applies DuckDB-specific
// DDL handling before delegating to ent's migrator.
type Schema struct {
	drv    dialect.Driver
	tables []*schema.Table
}

// NewSchema creates a schema manager from a generated ent driver and Tables
// variable, for example migrate.NewSchema(client.Driver(), entmigrate.Tables).
func NewSchema(drv dialect.Driver, tables []*schema.Table) *Schema {
	return &Schema{drv: drv, tables: tables}
}

// Create creates or migrates all schema resources.
func (s *Schema) Create(ctx context.Context, opts ...schema.MigrateOption) error {
	return Create(ctx, s.drv, s.tables, opts...)
}

// WriteTo writes DuckDB-compatible schema changes to w.
func (s *Schema) WriteTo(ctx context.Context, w io.Writer, opts ...schema.MigrateOption) error {
	drv := &schema.WriteDriver{Writer: rewriteWriter{w: w}, Driver: s.drv}
	return Create(ctx, drv, s.tables, opts...)
}

type rewriteWriter struct {
	w io.Writer
}

func (w rewriteWriter) Write(p []byte) (int, error) {
	_, err := io.WriteString(w.w, entduck.RewriteSQL(string(p)))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func createMissingTables(ctx context.Context, drv dialect.Driver, tables []*schema.Table) (map[string]bool, error) {
	ordered, err := orderTables(tables)
	if err != nil {
		return nil, err
	}
	tx, err := drv.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("entduck/migrate: begin schema create: %w", err)
	}
	created := make(map[string]bool)
	for _, t := range ordered {
		exists, err := tableExists(ctx, tx, t.Name)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if exists {
			continue
		}
		for _, c := range t.Columns {
			if !c.Increment {
				continue
			}
			query := fmt.Sprintf(`CREATE SEQUENCE IF NOT EXISTS "%s"`, sequenceName(t.Name, c.Name))
			if err := tx.Exec(ctx, query, []any{}, nil); err != nil {
				_ = tx.Rollback()
				return nil, fmt.Errorf("entduck/migrate: create sequence for %q.%q: %w", t.Name, c.Name, err)
			}
		}
		query, args := createTable(t).Query()
		if err := tx.Exec(ctx, entduck.RewriteSQL(query), args, nil); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("entduck/migrate: create table %q: %w", t.Name, err)
		}
		for _, idx := range t.Indexes {
			query, args := createIndex(t.Name, idx).Query()
			if err := tx.Exec(ctx, entduck.RewriteSQL(query), args, nil); err != nil {
				_ = tx.Rollback()
				return nil, fmt.Errorf("entduck/migrate: create index %q: %w", idx.Name, err)
			}
		}
		created[t.Name] = true
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("entduck/migrate: commit schema create: %w", err)
	}
	return created, nil
}

func filterCreatedTables(tables []*schema.Table, created map[string]bool) []*schema.Table {
	if len(created) == 0 {
		return tables
	}
	filtered := make([]*schema.Table, 0, len(tables)-len(created))
	for _, t := range tables {
		if !created[t.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func tableExists(ctx context.Context, drv dialect.ExecQuerier, name string) (bool, error) {
	rows := &entsql.Rows{}
	err := drv.Query(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = CURRENT_SCHEMA() AND table_name = $1`, []any{name}, rows)
	if err != nil {
		return false, fmt.Errorf("entduck/migrate: inspect table %q: %w", name, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return false, rows.Err()
	}
	var n int
	if err := rows.Scan(&n); err != nil {
		return false, fmt.Errorf("entduck/migrate: scan table %q existence: %w", name, err)
	}
	return n > 0, rows.Err()
}

func createTable(t *schema.Table) *entsql.TableBuilder {
	b := entsql.Dialect(dialect.Postgres).CreateTable(t.Name).IfNotExists()
	for _, c := range t.Columns {
		b.Column(addColumn(t.Name, c))
	}
	for _, pk := range t.PrimaryKey {
		b.PrimaryKey(pk.Name)
	}
	for _, fk := range t.ForeignKeys {
		b.Constraints(fk.DSL())
	}
	addChecks(b, t)
	return b
}

func createIndex(table string, idx *schema.Index) *entsql.IndexBuilder {
	b := entsql.Dialect(dialect.Postgres).CreateIndex(idx.Name).IfNotExists().Table(table)
	if idx.Unique {
		b.Unique()
	}
	for _, c := range idx.Columns {
		b.Column(c.Name)
	}
	return b
}

func addColumn(table string, c *schema.Column) *entsql.ColumnBuilder {
	b := entsql.Dialect(dialect.Postgres).Column(c.Name).Type(columnType(c)).Attr(c.Attr)
	if c.Unique {
		b.Attr("UNIQUE")
	}
	if c.Increment {
		b.Attr(fmt.Sprintf("DEFAULT nextval('%s')", sequenceName(table, c.Name)))
	}
	if c.Nullable {
		b.Attr("NULL")
	} else {
		b.Attr("NOT NULL")
	}
	writeDefault(b, c, "DEFAULT")
	if c.Collation != "" {
		b.Attr("COLLATE " + strconv.Quote(c.Collation))
	}
	return b
}

func sequenceName(table, column string) string {
	return table + "_" + column + "_seq"
}

func columnType(c *schema.Column) string {
	if c.SchemaType != nil && c.SchemaType[dialect.Postgres] != "" {
		return c.SchemaType[dialect.Postgres]
	}
	switch c.Type {
	case field.TypeBool:
		return "boolean"
	case field.TypeUint8, field.TypeInt8, field.TypeInt16, field.TypeUint16:
		return "smallint"
	case field.TypeInt32, field.TypeUint32:
		return "int"
	case field.TypeInt, field.TypeUint, field.TypeInt64, field.TypeUint64:
		return "bigint"
	case field.TypeFloat32:
		return "real"
	case field.TypeFloat64:
		return "double precision"
	case field.TypeBytes:
		return "bytea"
	case field.TypeJSON:
		return "json"
	case field.TypeUUID:
		return "uuid"
	case field.TypeString:
		if c.Size > 0 && c.Size < 1<<16 {
			return fmt.Sprintf("varchar(%d)", c.Size)
		}
		return "varchar"
	case field.TypeTime:
		return "timestamp with time zone"
	case field.TypeEnum:
		return "varchar"
	case field.TypeOther:
		return c.SchemaType[dialect.Postgres]
	default:
		panic(fmt.Sprintf("entduck/migrate: unsupported column type %q for %q", c.Type.String(), c.Name))
	}
}

func writeDefault(b *entsql.ColumnBuilder, c *schema.Column, clause string) {
	if c.Default == nil || !supportsDefault(c) {
		return
	}
	attr := fmt.Sprint(c.Default)
	switch v := c.Default.(type) {
	case bool:
		attr = strconv.FormatBool(v)
	case string:
		if t := c.Type; t != field.TypeUUID && t != field.TypeTime && !t.Numeric() {
			attr = fmt.Sprintf("'%s'", strings.ReplaceAll(v, "'", "''"))
		}
	}
	b.Attr(clause + " " + attr)
}

func supportsDefault(c *schema.Column) bool {
	switch t := c.Type; t {
	case field.TypeString, field.TypeEnum:
		return c.Size < 1<<16
	case field.TypeBool, field.TypeTime, field.TypeUUID:
		return true
	default:
		return t.Numeric()
	}
}

func addChecks(b *entsql.TableBuilder, t *schema.Table) {
	if t.Annotation == nil {
		return
	}
	if check := t.Annotation.Check; check != "" {
		b.Checks(func(b *entsql.Builder) {
			b.WriteString("CHECK " + checkExpr(check))
		})
	}
	names := make([]string, 0, len(t.Annotation.Checks))
	for name := range t.Annotation.Checks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		name := name
		b.Checks(func(b *entsql.Builder) {
			b.WriteString("CONSTRAINT ").Ident(name).WriteString(" CHECK " + checkExpr(t.Annotation.Checks[name]))
		})
	}
}

func checkExpr(expr string) string {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "(") && !strings.HasSuffix(expr, ")") {
		expr = "(" + expr + ")"
	}
	return expr
}

func orderTables(tables []*schema.Table) ([]*schema.Table, error) {
	byName := make(map[string]*schema.Table, len(tables))
	for _, t := range tables {
		byName[t.Name] = t
	}
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	ordered := make([]*schema.Table, 0, len(tables))
	var visit func(*schema.Table) error
	visit = func(t *schema.Table) error {
		if visited[t.Name] {
			return nil
		}
		if visiting[t.Name] {
			return fmt.Errorf("entduck/migrate: circular foreign keys involving table %q cannot be created inline because DuckDB does not support ALTER TABLE ADD CONSTRAINT", t.Name)
		}
		visiting[t.Name] = true
		for _, fk := range t.ForeignKeys {
			if fk.RefTable == nil || fk.RefTable.Name == t.Name {
				continue
			}
			if ref := byName[fk.RefTable.Name]; ref != nil {
				if err := visit(ref); err != nil {
					return err
				}
			}
		}
		visiting[t.Name] = false
		visited[t.Name] = true
		ordered = append(ordered, t)
		return nil
	}
	for _, t := range tables {
		if err := visit(t); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}
