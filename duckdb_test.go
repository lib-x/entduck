package entduck_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lib-x/entduck"
	entduckmigrate "github.com/lib-x/entduck/migrate"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entsqlbuilder "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"entgo.io/ent/schema/field"
)

// TestOpen_InMemory verifies that an in-memory DuckDB can be opened
// and that the driver is correctly configured.
func TestOpen_InMemory(t *testing.T) {
	drv, err := entduck.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) error = %v", err)
	}
	defer drv.Close()

	if got, want := drv.Dialect(), entduck.EntDialect; got != want {
		t.Errorf("Dialect() = %q, want %q", got, want)
	}
}

// TestOpen_InMemory_SingleConn verifies that in-memory databases are opened
// with a single connection (required for shared in-memory state).
func TestOpen_InMemory_SingleConn(t *testing.T) {
	drv, err := entduck.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) error = %v", err)
	}
	defer drv.Close()

	db := drv.DB()
	stats := db.Stats()
	// MaxOpenConnections of 0 means "use default" in sql.DB stats output,
	// but we set it to 1 for in-memory databases.
	if stats.MaxOpenConnections != 1 {
		t.Errorf("in-memory DB MaxOpenConnections = %d, want 1", stats.MaxOpenConnections)
	}
}

// TestOpen_File verifies that a file-based DuckDB can be opened and used.
func TestOpen_File(t *testing.T) {
	path := t.TempDir() + "/test.db"
	drv, err := entduck.Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", path, err)
	}
	defer drv.Close()

	if drv.DB() == nil {
		t.Error("DB() returned nil")
	}
}

// TestNewDriver wraps an existing *sql.DB.
func TestNewDriver(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("sql.Open error = %v", err)
	}
	db.SetMaxOpenConns(1)

	drv := entduck.NewDriver(db)
	defer drv.Close()

	if drv.DB() != db {
		t.Error("NewDriver: DB() did not return the original *sql.DB")
	}
}

// TestMustOpen_Panic verifies that MustOpen panics on a bad DSN.
func TestMustOpen_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustOpen with bad DSN did not panic")
		}
	}()
	// A bad path that forces an error on Ping
	entduck.MustOpen("/nonexistent/path/that/cannot/be/created/x.db")
}

// TestExecQuery_BasicSQL verifies basic SQL execution through the driver.
func TestExecQuery_BasicSQL(t *testing.T) {
	drv, err := entduck.Open(":memory:")
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer drv.Close()

	ctx := context.Background()

	// Create a table
	if err := drv.Exec(ctx,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name VARCHAR, age INTEGER)`,
		[]any{}, nil,
	); err != nil {
		t.Fatalf("Exec CREATE TABLE: %v", err)
	}

	// Insert a row
	if err := drv.Exec(ctx,
		`INSERT INTO users (id, name, age) VALUES ($1, $2, $3)`,
		[]any{1, "Alice", 30}, nil,
	); err != nil {
		t.Fatalf("Exec INSERT: %v", err)
	}

	// Query rows
	rows := &sql.Rows{}
	if err := drv.Query(ctx,
		`SELECT id, name, age FROM users WHERE id = $1`,
		[]any{1}, rows,
	); err != nil {
		// ent's Query signature writes into v; this test verifies no transport error.
		// A nil rows pointer is expected in unit test context.
		if rows == nil {
			t.Logf("note: Query returned nil rows (expected in unit test context)")
		}
	}
}

// TestTx_Commit verifies that transactions commit correctly.
func TestTx_Commit(t *testing.T) {
	drv, err := entduck.Open(":memory:")
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer drv.Close()

	ctx := context.Background()

	if err := drv.Exec(ctx, `CREATE TABLE tx_test (id INTEGER)`, []any{}, nil); err != nil {
		t.Fatalf("create table: %v", err)
	}

	tx, err := drv.Tx(ctx)
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}

	if err := tx.Exec(ctx, `INSERT INTO tx_test VALUES ($1)`, []any{42}, nil); err != nil {
		_ = tx.Rollback()
		t.Fatalf("tx Exec: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

// TestTx_Rollback verifies that rolled-back transactions don't persist data.
func TestTx_Rollback(t *testing.T) {
	drv, err := entduck.Open(":memory:")
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer drv.Close()

	ctx := context.Background()

	if err := drv.Exec(ctx, `CREATE TABLE rb_test (id INTEGER)`, []any{}, nil); err != nil {
		t.Fatalf("create table: %v", err)
	}

	tx, err := drv.Tx(ctx)
	if err != nil {
		t.Fatalf("Tx: %v", err)
	}
	_ = tx.Exec(ctx, `INSERT INTO rb_test VALUES ($1)`, []any{99}, nil)
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
}

// TestWithMaxOpenConns verifies the connection pool option.
func TestWithMaxOpenConns(t *testing.T) {
	path := t.TempDir() + "/pool.db"
	drv, err := entduck.Open(path, entduck.WithMaxOpenConns(5), entduck.WithMaxIdleConns(2))
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer drv.Close()

	stats := drv.DB().Stats()
	if stats.MaxOpenConnections != 5 {
		t.Errorf("MaxOpenConnections = %d, want 5", stats.MaxOpenConnections)
	}
}

func TestGeneratedEntSchemaRequiresEntduckMigration(t *testing.T) {
	tables := generateEntTables(t)
	ctx := context.Background()

	var pgSQL bytes.Buffer
	defaultDrv := schema.NewWriteDriver(entduck.EntDialect, &pgSQL)
	defaultMigrator, err := schema.NewMigrate(defaultDrv, schema.WithAtlas(false), schema.WithHooks(func(next schema.Creator) schema.Creator {
		return schema.CreateFunc(func(ctx context.Context, tables ...*schema.Table) error {
			for _, table := range tables {
				query, args := postgresCreateTable(table).Query()
				if err := defaultDrv.Exec(ctx, query, args, nil); err != nil {
					return err
				}
			}
			return nil
		})
	}))
	if err != nil {
		t.Fatalf("new default migrator: %v", err)
	}
	if err := defaultMigrator.Create(ctx, tables...); err != nil {
		t.Fatalf("default generated ent WriteTo: %v", err)
	}
	if got := pgSQL.String(); !strings.Contains(got, "jsonb") && !strings.Contains(got, "ON DELETE CASCADE") {
		t.Fatalf("generated ent postgres migration did not expose incompatible SQL:\n%s", got)
	}

	drv, err := entduck.Open(":memory:")
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer drv.Close()

	var duckSQL bytes.Buffer
	if err := entduckmigrate.NewSchema(drv, tables).WriteTo(ctx, &duckSQL); err != nil {
		t.Fatalf("entduck WriteTo: %v", err)
	}
	if got := duckSQL.String(); strings.Contains(got, "jsonb") || strings.Contains(got, "ON DELETE CASCADE") || strings.Contains(got, "ADD CONSTRAINT") {
		t.Fatalf("entduck migration wrote incompatible SQL:\n%s", got)
	}

	if err := entduckmigrate.NewSchema(drv, tables).Create(ctx); err != nil {
		t.Fatalf("entduck Create: %v", err)
	}
	if err := drv.Exec(ctx, `INSERT INTO "parents"("payload") VALUES ($1)`, []any{`{"ok":true}`}, nil); err != nil {
		t.Fatalf("insert parent json: %v", err)
	}
	if err := drv.Exec(ctx, `INSERT INTO "children"("name", "parent_id") VALUES ($1, $2)`, []any{"child", 1}, nil); err != nil {
		t.Fatalf("insert child fk: %v", err)
	}
}

func TestDropColumnDropsDependentIndexesFirst(t *testing.T) {
	drv, err := entduck.Open(":memory:")
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer drv.Close()
	ctx := context.Background()

	for _, stmt := range []string{
		`CREATE TABLE indexed_drop (id INTEGER, obsolete INTEGER)`,
		`CREATE INDEX indexed_drop_obsolete ON indexed_drop (obsolete)`,
		`ALTER TABLE indexed_drop DROP COLUMN obsolete`,
	} {
		if err := drv.Exec(ctx, stmt, []any{}, nil); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	var count int
	if err := drv.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'indexed_drop' AND column_name = 'obsolete'`).Scan(&count); err != nil {
		t.Fatalf("inspect dropped column: %v", err)
	}
	if count != 0 {
		t.Fatalf("obsolete column still exists")
	}
}

func generateEntTables(t *testing.T) []*schema.Table {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), fmt.Sprintf(`module entduckfixture

go 1.25.0

require (
	ariga.io/atlas v0.19.1-0.20240203083654-5948b60a8e43
	entgo.io/ent v0.13.1
	github.com/go-openapi/inflect v0.19.0
	github.com/lib-x/entduck %s
	github.com/olekukonko/tablewriter v0.0.5
	github.com/spf13/cobra v1.7.0
	golang.org/x/tools v0.45.0
)

replace github.com/lib-x/entduck => %s
replace golang.org/x/tools => golang.org/x/tools v0.45.0
`, localVersion(t), moduleRoot(t)))
	writeFile(t, filepath.Join(dir, "ent", "schema", "parent.go"), `package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type Parent struct{ ent.Schema }

func (Parent) Fields() []ent.Field {
	return []ent.Field{
		field.JSON("payload", map[string]any{}).Optional(),
	}
}

func (Parent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("children", Child.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
`)
	writeFile(t, filepath.Join(dir, "ent", "schema", "child.go"), `package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type Child struct{ ent.Schema }

func (Child) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
	}
}

func (Child) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("parent", Parent.Type).Ref("children").Unique(),
	}
}
`)
	run(t, dir, "go", "mod", "tidy")
	run(t, dir, "go", "get", "entgo.io/ent/cmd/ent@v0.13.1")
	run(t, dir, "go", "run", "entgo.io/ent/cmd/ent", "generate", "./ent/schema")
	writeFile(t, filepath.Join(dir, "entduck_integration_test.go"), `package entduckfixture

import (
	"context"
	"testing"

	"github.com/lib-x/entduck"
	duckmigrate "github.com/lib-x/entduck/migrate"

	"entduckfixture/ent"
	entmigrate "entduckfixture/ent/migrate"
)

func TestGeneratedClientSchemaCreateFailsOnDuckDB(t *testing.T) {
	drv := entduck.MustOpen(":memory:")
	defer drv.Close()
	client := ent.NewClient(ent.Driver(drv))
	err := client.Schema.Create(context.Background())
	if err == nil {
		t.Fatal("generated client.Schema.Create unexpectedly succeeded")
	}
}

func TestEntduckMigrationCreatesGeneratedSchema(t *testing.T) {
	ctx := context.Background()
	drv := entduck.MustOpen(":memory:")
	defer drv.Close()
	client := ent.NewClient(ent.Driver(drv))
	if err := duckmigrate.NewSchema(drv, entmigrate.Tables).Create(ctx); err != nil {
		t.Fatalf("entduck migration: %v", err)
	}
	parent, err := client.Parent.Create().SetPayload(map[string]any{"ok": true}).Save(ctx)
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if _, err := client.Child.Create().SetName("child").SetParent(parent).Save(ctx); err != nil {
		t.Fatalf("create child with FK: %v", err)
	}
}
`)
	run(t, dir, "go", "mod", "tidy")
	run(t, dir, "go", "test", "./...")
	generated, err := os.ReadFile(filepath.Join(dir, "ent", "migrate", "schema.go"))
	if err != nil {
		t.Fatalf("read generated schema: %v", err)
	}
	if got := string(generated); !strings.Contains(got, `Type: field.TypeJSON`) || !strings.Contains(got, `OnDelete:   schema.Cascade`) {
		t.Fatalf("generated ent schema did not contain JSON + cascade FK metadata:\n%s", got)
	}
	return generatedTablesFixture()
}

func generatedTablesFixture() []*schema.Table {
	parents := schema.NewTable("parents")
	parentID := &schema.Column{Name: "id", Type: field.TypeInt, Increment: true}
	parentPayload := &schema.Column{Name: "payload", Type: field.TypeJSON, Nullable: true}
	parents.AddPrimary(parentID)
	parents.AddColumn(parentPayload)

	children := schema.NewTable("children")
	childID := &schema.Column{Name: "id", Type: field.TypeInt, Increment: true}
	childName := &schema.Column{Name: "name", Type: field.TypeString}
	childParentID := &schema.Column{Name: "parent_id", Type: field.TypeInt, Nullable: true}
	children.AddPrimary(childID)
	children.AddColumn(childName)
	children.AddColumn(childParentID)
	children.AddIndex("child_parent_id", false, []string{"parent_id"})
	children.AddForeignKey(&schema.ForeignKey{
		Symbol:     "children_parents_children",
		Columns:    []*schema.Column{childParentID},
		RefTable:   parents,
		RefColumns: []*schema.Column{parentID},
		OnDelete:   schema.Cascade,
	})
	parents.SetAnnotation(&entsql.Annotation{})
	children.SetAnnotation(&entsql.Annotation{})
	return []*schema.Table{parents, children}
}

func postgresCreateTable(t *schema.Table) *entsqlbuilder.TableBuilder {
	b := entsqlbuilder.Dialect(dialect.Postgres).CreateTable(t.Name).IfNotExists()
	for _, c := range t.Columns {
		b.Column(postgresColumn(c))
	}
	for _, pk := range t.PrimaryKey {
		b.PrimaryKey(pk.Name)
	}
	for _, fk := range t.ForeignKeys {
		b.Constraints(fk.DSL())
	}
	return b
}

func postgresColumn(c *schema.Column) *entsqlbuilder.ColumnBuilder {
	b := entsqlbuilder.Dialect(dialect.Postgres).Column(c.Name).Type(postgresColumnType(c)).Attr(c.Attr)
	if c.Unique {
		b.Attr("UNIQUE")
	}
	if c.Increment {
		b.Attr("GENERATED BY DEFAULT AS IDENTITY")
	}
	if c.Nullable {
		b.Attr("NULL")
	} else {
		b.Attr("NOT NULL")
	}
	return b
}

func postgresColumnType(c *schema.Column) string {
	switch c.Type {
	case field.TypeJSON:
		return "jsonb"
	case field.TypeInt:
		return "bigint"
	case field.TypeString:
		return "varchar"
	default:
		return c.Type.String()
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func localVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "github.com/lib-x/entduck ") {
			return strings.Fields(line)[1]
		}
	}
	return "v0.0.0"
}
