// Package entduck provides an ent ORM driver for DuckDB.
//
// DuckDB is an in-process SQL OLAP database management system. This package
// wraps the standard ent SQL driver and adapts it to work with DuckDB by
// leveraging DuckDB's high compatibility with PostgreSQL SQL syntax.
//
// # Quick Start
//
//	// Open an in-memory database
//	drv, err := entduck.Open(":memory:")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer drv.Close()
//
//	// Create an ent client using the generated NewClient constructor
//	client := ent.NewClient(ent.Driver(drv))
//
//	// Run schema migration through github.com/lib-x/entduck/migrate.
//	if err := migrate.NewSchema(drv, generatedmigrate.Tables).Create(context.Background()); err != nil {
//	    log.Fatal(err)
//	}
//
// # Dialect Compatibility
//
// DuckDB is compatible with much of PostgreSQL SQL syntax. Generate ent schemas
// with SQL storage and use entduck/migrate for schema creation so PostgreSQL
// DDL features not implemented by DuckDB are normalized.
//
// # Connection Pooling
//
// For in-memory databases, the driver automatically limits MaxOpenConns to 1
// because each DuckDB connection gets its own separate in-memory database.
// For file-based databases, standard connection pool settings apply but note
// that DuckDB only allows a single write connection at a time by default.
package entduck

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	// Register the DuckDB driver with database/sql under the name "duckdb".
	_ "github.com/marcboeker/go-duckdb"
)

const (
	// DriverName is the name DuckDB registers under with database/sql.
	DriverName = "duckdb"

	// EntDialect is the ent SQL dialect used for query and schema generation.
	// DuckDB closely follows PostgreSQL syntax, so postgres is used internally.
	EntDialect = dialect.Postgres
)

// Driver is the ent ORM driver for DuckDB. It reports the postgres dialect to
// ent and rewrites the PostgreSQL SQL fragments DuckDB does not support.
type Driver struct {
	db   *sql.DB
	opts options
}

// options holds internal configuration for the Driver.
type options struct {
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime int // seconds, 0 = unlimited
}

// Option is a functional option for configuring the Driver.
type Option func(*options)

// WithMaxOpenConns sets the maximum number of open connections to the database.
// For in-memory databases this is capped to 1 regardless of this setting.
func WithMaxOpenConns(n int) Option {
	return func(o *options) { o.maxOpenConns = n }
}

// WithMaxIdleConns sets the maximum number of idle connections in the pool.
func WithMaxIdleConns(n int) Option {
	return func(o *options) { o.maxIdleConns = n }
}

// Open opens a DuckDB database and returns an ent-compatible Driver.
//
// Supported DSN formats:
//
//	":memory:"            — transient in-memory database (single connection enforced)
//	"path/to/file.db"     — persistent file database
//	"path/to/file.db?threads=4&access_mode=read_only"  — with DuckDB config options
//
// Common DuckDB config options (appended as query parameters):
//   - threads=N            — number of worker threads
//   - access_mode=read_only — open in read-only mode
//   - memory_limit=1GB     — memory usage limit
func Open(dsn string, opts ...Option) (*Driver, error) {
	o := defaultOptions(dsn)
	for _, opt := range opts {
		opt(&o)
	}

	openDSN := dsn
	if isMemoryDSN(dsn) {
		openDSN = ""
	}
	db, err := sql.Open(DriverName, openDSN)
	if err != nil {
		return nil, fmt.Errorf("entduck: open %q: %w", dsn, err)
	}

	applyPoolSettings(db, o)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("entduck: ping %q: %w", dsn, err)
	}

	return newDriver(db, o), nil
}

// NewDriver wraps an existing *sql.DB in an ent Driver.
// The db must have been opened with the "duckdb" sql driver.
//
//	db, _ := sql.Open("duckdb", ":memory:")
//	drv := entduck.NewDriver(db)
func NewDriver(db *sql.DB, opts ...Option) *Driver {
	o := defaultOptions("")
	for _, opt := range opts {
		opt(&o)
	}
	return newDriver(db, o)
}

// MustOpen opens a DuckDB database or panics on failure.
// Useful for top-level initialization where error handling is impractical.
func MustOpen(dsn string, opts ...Option) *Driver {
	drv, err := Open(dsn, opts...)
	if err != nil {
		panic(fmt.Sprintf("entduck: %v", err))
	}
	return drv
}

// Dialect returns the SQL dialect identifier used by ent for schema generation.
// Returns "postgres" because DuckDB's SQL dialect is largely PostgreSQL-compatible.
// When using ent codegen, configure your schema with dialect.Postgres.
func (d *Driver) Dialect() string {
	return EntDialect
}

// DB returns the underlying *sql.DB connection pool.
func (d *Driver) DB() *sql.DB {
	return d.db
}

// Tx returns a new transactional driver within the given context.
func (d *Driver) Tx(ctx context.Context) (dialect.Tx, error) {
	return d.BeginTx(ctx, nil)
}

// BeginTx begins a transaction with the given TxOptions.
// Allows controlling isolation level and read-only mode.
func (d *Driver) BeginTx(ctx context.Context, opts *sql.TxOptions) (dialect.Tx, error) {
	tx, err := d.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx}, nil
}

// Close closes the driver and releases all database connections.
func (d *Driver) Close() error {
	return d.db.Close()
}

// Exec implements dialect.ExecQuerier with DuckDB compatibility rewrites.
func (d *Driver) Exec(ctx context.Context, query string, args, v any) error {
	argv, ok := args.([]any)
	if !ok {
		return fmt.Errorf("entduck: invalid args type %T, want []any", args)
	}
	if err := dropIndexesForDropColumn(ctx, d.db, query); err != nil {
		return err
	}
	query = RewriteSQL(query)
	switch v := v.(type) {
	case nil:
		_, err := d.db.ExecContext(ctx, query, argv...)
		return err
	case *sql.Result:
		res, err := d.db.ExecContext(ctx, query, argv...)
		if err != nil {
			return err
		}
		*v = res
		return nil
	default:
		return fmt.Errorf("entduck: invalid result type %T, want *sql.Result", v)
	}
}

// Query implements dialect.ExecQuerier with DuckDB compatibility rewrites.
func (d *Driver) Query(ctx context.Context, query string, args, v any) error {
	rows, ok := v.(*entsql.Rows)
	if !ok {
		return fmt.Errorf("entduck: invalid rows type %T, want *sql.Rows", v)
	}
	argv, ok := args.([]any)
	if !ok {
		return fmt.Errorf("entduck: invalid args type %T, want []any", args)
	}
	if isPostgresVersionQuery(query) {
		*rows = entsql.Rows{ColumnScanner: newSingleValueRows("server_version_num", "150002")}
		return nil
	}
	rs, err := d.db.QueryContext(ctx, RewriteSQL(query), argv...)
	if err != nil {
		return err
	}
	*rows = entsql.Rows{ColumnScanner: rs}
	return nil
}

// ExecContext executes a query without returning rows.
func (d *Driver) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := dropIndexesForDropColumn(ctx, d.db, query); err != nil {
		return nil, err
	}
	return d.db.ExecContext(ctx, RewriteSQL(query), args...)
}

// QueryContext executes a query that returns rows.
func (d *Driver) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.db.QueryContext(ctx, RewriteSQL(query), args...)
}

// Tx is a DuckDB transaction that applies the same SQL compatibility rewrites
// as the parent Driver.
type Tx struct {
	tx *sql.Tx
}

// Exec implements dialect.ExecQuerier within a transaction.
func (t *Tx) Exec(ctx context.Context, query string, args, v any) error {
	argv, ok := args.([]any)
	if !ok {
		return fmt.Errorf("entduck: invalid args type %T, want []any", args)
	}
	if err := dropIndexesForDropColumn(ctx, t.tx, query); err != nil {
		return err
	}
	query = RewriteSQL(query)
	switch v := v.(type) {
	case nil:
		_, err := t.tx.ExecContext(ctx, query, argv...)
		return err
	case *sql.Result:
		res, err := t.tx.ExecContext(ctx, query, argv...)
		if err != nil {
			return err
		}
		*v = res
		return nil
	default:
		return fmt.Errorf("entduck: invalid result type %T, want *sql.Result", v)
	}
}

// Query implements dialect.ExecQuerier within a transaction.
func (t *Tx) Query(ctx context.Context, query string, args, v any) error {
	rows, ok := v.(*entsql.Rows)
	if !ok {
		return fmt.Errorf("entduck: invalid rows type %T, want *sql.Rows", v)
	}
	argv, ok := args.([]any)
	if !ok {
		return fmt.Errorf("entduck: invalid args type %T, want []any", args)
	}
	if isPostgresVersionQuery(query) {
		*rows = entsql.Rows{ColumnScanner: newSingleValueRows("server_version_num", "150002")}
		return nil
	}
	rs, err := t.tx.QueryContext(ctx, RewriteSQL(query), argv...)
	if err != nil {
		return err
	}
	*rows = entsql.Rows{ColumnScanner: rs}
	return nil
}

// Commit commits the transaction.
func (t *Tx) Commit() error { return t.tx.Commit() }

// Rollback aborts the transaction.
func (t *Tx) Rollback() error { return t.tx.Rollback() }

var _ dialect.Driver = (*Driver)(nil)
var _ dialect.Tx = (*Tx)(nil)
var _ driver.Tx = (*Tx)(nil)

// --- internal helpers ---

func newDriver(db *sql.DB, o options) *Driver {
	return &Driver{
		db:   db,
		opts: o,
	}
}

func defaultOptions(dsn string) options {
	o := options{
		maxOpenConns: 0,
		maxIdleConns: 2,
	}
	// In-memory databases share state only within a single connection.
	// Multiple connections each get a separate in-memory DB.
	if isMemoryDSN(dsn) {
		o.maxOpenConns = 1
		o.maxIdleConns = 1
	}
	return o
}

func isMemoryDSN(dsn string) bool {
	return dsn == "" || dsn == ":memory:" ||
		strings.HasPrefix(dsn, ":memory:?") ||
		strings.HasPrefix(dsn, "file::memory:")
}

func applyPoolSettings(db *sql.DB, o options) {
	if o.maxOpenConns > 0 {
		db.SetMaxOpenConns(o.maxOpenConns)
	}
	if o.maxIdleConns > 0 {
		db.SetMaxIdleConns(o.maxIdleConns)
	}
}
