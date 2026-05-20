# entduck — DuckDB driver for ent ORM

[![Go Reference](https://pkg.go.dev/badge/github.com/lib-x/entduck.svg)](https://pkg.go.dev/github.com/lib-x/entduck)

`entduck` provides an [ent ORM](https://entgo.io) driver for [DuckDB](https://duckdb.org). DuckDB is an in-process analytical SQL database that supports complex queries over large datasets with minimal operational overhead.

---

## Installation

```bash
go get github.com/lib-x/entduck
```

---

## Quick Start

### 1. Define your ent schema

```go
// ent/schema/user.go
package schema

import (
    "entgo.io/ent"
    "entgo.io/ent/schema/field"
)

type User struct{ ent.Schema }

func (User) Fields() []ent.Field {
    return []ent.Field{
        field.Int("id"),
        field.String("name"),
        field.String("email").Unique(),
        field.Int("age").Optional(),
    }
}
```

### 2. Generate ent code

Generate normal ent SQL code:

```bash
go run entgo.io/ent/cmd/ent generate ./ent/schema
```

Or with `go generate`:

```go
//go:generate go run entgo.io/ent/cmd/ent generate ./ent/schema
```

### 3. Wire up the driver

```go
package main

import (
    "context"
    "log"

    "github.com/lib-x/entduck"
    duckmigrate "github.com/lib-x/entduck/migrate"
    "your-module/ent"
    entmigrate "your-module/ent/migrate"
)

func main() {
    // In-memory database (ideal for development and testing)
    drv, err := entduck.Open(":memory:")

    // File-based persistent database
    // drv, err := entduck.Open("./myapp.duckdb")

    if err != nil {
        log.Fatal(err)
    }
    defer drv.Close()

    client := ent.NewClient(ent.Driver(drv))
    ctx := context.Background()

    // Auto-migrate schema. Use entduck/migrate instead of client.Schema.Create.
    if err := duckmigrate.NewSchema(drv, entmigrate.Tables).Create(ctx); err != nil {
        log.Fatalf("schema migration: %v", err)
    }

    // Create a user
    u, err := client.User.Create().
        SetName("Alice").
        SetEmail("alice@example.com").
        SetAge(30).
        Save(ctx)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("created user %d: %s", u.ID, u.Name)
}
```

For a local persistent database file:

```go
drv, err := entduck.Open(
    "./data/app.duckdb?threads=4&memory_limit=2GB&access_mode=read_write&temp_directory=./data/tmp",
    entduck.WithMaxOpenConns(1),
    entduck.WithMaxIdleConns(1),
)
if err != nil {
    log.Fatal(err)
}
defer drv.Close()
```

DuckDB opens a missing file path by creating the database file. For write-heavy
applications, keep `MaxOpenConns(1)` unless you have explicitly validated your
workload against DuckDB's single-writer model.

---

## API Reference

### `entduck.Open(dsn string, opts ...Option) (*Driver, error)`

Opens a DuckDB database and returns an ent-compatible driver.

| DSN | Description |
|-----|-------------|
| `":memory:"` | Transient in-memory database. Automatically uses `MaxOpenConns(1)`. |
| `"./data.duckdb"` | Persistent file database. |
| `"./data.duckdb?threads=4"` | With DuckDB configuration options. |
| `"./data.duckdb?access_mode=read_only"` | Read-only mode. |

DSN query parameters are DuckDB configuration settings. The go-duckdb driver
passes them to DuckDB when opening the database. Values must be URL-encoded
when they contain spaces, commas, or path separators that need escaping.

Examples:

```text
./data.duckdb?access_mode=read_only
./data.duckdb?access_mode=read_write&threads=8&memory_limit=4GB
./data.duckdb?temp_directory=./tmp&max_temp_directory_size=20GB
./data.duckdb?enable_external_access=false&autoload_known_extensions=false
```

See DuckDB's official configuration docs:
https://duckdb.org/docs/stable/configuration/overview.html

<details>
<summary>DuckDB DSN Configuration Parameters</summary>

This list is generated from `duckdb_settings()` for the embedded DuckDB used by
`github.com/marcboeker/go-duckdb v1.8.1`.

| Parameter | Type | Scope |
| --- | --- | --- |
| `access_mode` | `VARCHAR` | `GLOBAL` |
| `allocator_background_threads` | `BOOLEAN` | `GLOBAL` |
| `allocator_bulk_deallocation_flush_threshold` | `VARCHAR` | `GLOBAL` |
| `allocator_flush_threshold` | `VARCHAR` | `GLOBAL` |
| `allow_community_extensions` | `BOOLEAN` | `GLOBAL` |
| `allow_extensions_metadata_mismatch` | `BOOLEAN` | `GLOBAL` |
| `allow_persistent_secrets` | `BOOLEAN` | `GLOBAL` |
| `allow_unredacted_secrets` | `BOOLEAN` | `GLOBAL` |
| `allow_unsigned_extensions` | `BOOLEAN` | `GLOBAL` |
| `arrow_large_buffer_size` | `BOOLEAN` | `GLOBAL` |
| `arrow_lossless_conversion` | `BOOLEAN` | `GLOBAL` |
| `arrow_output_list_view` | `BOOLEAN` | `GLOBAL` |
| `autoinstall_extension_repository` | `VARCHAR` | `GLOBAL` |
| `autoinstall_known_extensions` | `BOOLEAN` | `GLOBAL` |
| `autoload_known_extensions` | `BOOLEAN` | `GLOBAL` |
| `binary_as_string` | `BOOLEAN` | `GLOBAL` |
| `catalog_error_max_schemas` | `UBIGINT` | `GLOBAL` |
| `checkpoint_threshold` | `VARCHAR` | `GLOBAL` |
| `custom_extension_repository` | `VARCHAR` | `GLOBAL` |
| `custom_profiling_settings` | `VARCHAR` | `LOCAL` |
| `custom_user_agent` | `VARCHAR` | `GLOBAL` |
| `debug_asof_iejoin` | `BOOLEAN` | `LOCAL` |
| `debug_checkpoint_abort` | `VARCHAR` | `GLOBAL` |
| `debug_force_external` | `BOOLEAN` | `LOCAL` |
| `debug_force_no_cross_product` | `BOOLEAN` | `LOCAL` |
| `debug_skip_checkpoint_on_commit` | `BOOLEAN` | `GLOBAL` |
| `debug_window_mode` | `VARCHAR` | `GLOBAL` |
| `default_block_size` | `UBIGINT` | `GLOBAL` |
| `default_collation` | `VARCHAR` | `GLOBAL` |
| `default_null_order` | `VARCHAR` | `GLOBAL` |
| `default_order` | `VARCHAR` | `GLOBAL` |
| `default_secret_storage` | `VARCHAR` | `GLOBAL` |
| `disabled_filesystems` | `VARCHAR` | `GLOBAL` |
| `disabled_optimizers` | `VARCHAR` | `GLOBAL` |
| `duckdb_api` | `VARCHAR` | `GLOBAL` |
| `enable_external_access` | `BOOLEAN` | `GLOBAL` |
| `enable_fsst_vectors` | `BOOLEAN` | `GLOBAL` |
| `enable_http_logging` | `BOOLEAN` | `LOCAL` |
| `enable_http_metadata_cache` | `BOOLEAN` | `GLOBAL` |
| `enable_macro_dependencies` | `BOOLEAN` | `GLOBAL` |
| `enable_object_cache` | `BOOLEAN` | `GLOBAL` |
| `enable_profiling` | `VARCHAR` | `LOCAL` |
| `enable_progress_bar` | `BOOLEAN` | `LOCAL` |
| `enable_progress_bar_print` | `BOOLEAN` | `LOCAL` |
| `enable_view_dependencies` | `BOOLEAN` | `GLOBAL` |
| `errors_as_json` | `BOOLEAN` | `LOCAL` |
| `explain_output` | `VARCHAR` | `LOCAL` |
| `extension_directory` | `VARCHAR` | `GLOBAL` |
| `external_threads` | `BIGINT` | `GLOBAL` |
| `file_search_path` | `VARCHAR` | `LOCAL` |
| `force_bitpacking_mode` | `VARCHAR` | `GLOBAL` |
| `force_compression` | `VARCHAR` | `GLOBAL` |
| `home_directory` | `VARCHAR` | `LOCAL` |
| `http_logging_output` | `VARCHAR` | `LOCAL` |
| `http_proxy` | `VARCHAR` | `GLOBAL` |
| `http_proxy_password` | `VARCHAR` | `GLOBAL` |
| `http_proxy_username` | `VARCHAR` | `GLOBAL` |
| `ieee_floating_point_ops` | `BOOLEAN` | `LOCAL` |
| `immediate_transaction_mode` | `BOOLEAN` | `GLOBAL` |
| `index_scan_max_count` | `UBIGINT` | `GLOBAL` |
| `index_scan_percentage` | `DOUBLE` | `GLOBAL` |
| `integer_division` | `BOOLEAN` | `LOCAL` |
| `lock_configuration` | `BOOLEAN` | `GLOBAL` |
| `log_query_path` | `VARCHAR` | `LOCAL` |
| `max_expression_depth` | `UBIGINT` | `LOCAL` |
| `max_memory` | `VARCHAR` | `GLOBAL` |
| `max_temp_directory_size` | `VARCHAR` | `GLOBAL` |
| `max_vacuum_tasks` | `UBIGINT` | `GLOBAL` |
| `memory_limit` | `VARCHAR` | `GLOBAL` |
| `merge_join_threshold` | `UBIGINT` | `LOCAL` |
| `nested_loop_join_threshold` | `UBIGINT` | `LOCAL` |
| `null_order` | `VARCHAR` | `GLOBAL` |
| `old_implicit_casting` | `BOOLEAN` | `GLOBAL` |
| `order_by_non_integer_literal` | `BOOLEAN` | `LOCAL` |
| `ordered_aggregate_threshold` | `UBIGINT` | `LOCAL` |
| `partitioned_write_flush_threshold` | `UBIGINT` | `LOCAL` |
| `partitioned_write_max_open_files` | `UBIGINT` | `LOCAL` |
| `password` | `VARCHAR` | `GLOBAL` |
| `perfect_ht_threshold` | `BIGINT` | `LOCAL` |
| `pivot_filter_threshold` | `BIGINT` | `LOCAL` |
| `pivot_limit` | `BIGINT` | `LOCAL` |
| `prefer_range_joins` | `BOOLEAN` | `LOCAL` |
| `preserve_identifier_case` | `BOOLEAN` | `LOCAL` |
| `preserve_insertion_order` | `BOOLEAN` | `GLOBAL` |
| `produce_arrow_string_view` | `BOOLEAN` | `GLOBAL` |
| `profile_output` | `VARCHAR` | `LOCAL` |
| `profiling_mode` | `VARCHAR` | `LOCAL` |
| `profiling_output` | `VARCHAR` | `LOCAL` |
| `progress_bar_time` | `BIGINT` | `LOCAL` |
| `scalar_subquery_error_on_multiple_rows` | `BOOLEAN` | `LOCAL` |
| `schema` | `VARCHAR` | `LOCAL` |
| `search_path` | `VARCHAR` | `LOCAL` |
| `secret_directory` | `VARCHAR` | `GLOBAL` |
| `storage_compatibility_version` | `VARCHAR` | `GLOBAL` |
| `streaming_buffer_size` | `VARCHAR` | `LOCAL` |
| `temp_directory` | `VARCHAR` | `GLOBAL` |
| `threads` | `BIGINT` | `GLOBAL` |
| `user` | `VARCHAR` | `GLOBAL` |
| `username` | `VARCHAR` | `GLOBAL` |
| `wal_autocheckpoint` | `VARCHAR` | `GLOBAL` |
| `worker_threads` | `BIGINT` | `GLOBAL` |

</details>

### `entduck.NewDriver(db *sql.DB, opts ...Option) *Driver`

Wraps an existing `*sql.DB` that was opened with the `"duckdb"` driver. Useful when you need full control over the connection setup.

### `entduck.MustOpen(dsn string, opts ...Option) *Driver`

Like `Open` but panics on error. Suitable for top-level initialization.

### Options

| Option | Description |
|--------|-------------|
| `WithMaxOpenConns(n int)` | Maximum open connections. For in-memory DBs, capped to 1. |
| `WithMaxIdleConns(n int)` | Maximum idle connections in the pool. |

---

## DuckDB Native Types

Use DuckDB's rich type system in your ent schema fields with `SchemaType`:

```go
import "github.com/lib-x/entduck/ducktype"

// LIST (array) column
field.Strings("tags").
    SchemaType(ducktype.ForDuck(ducktype.ListOf("VARCHAR")))

// MAP column
field.String("metadata").
    SchemaType(map[string]string{
        dialect.Postgres: "MAP(VARCHAR, VARCHAR)",
    })

// STRUCT column
field.String("address").
    SchemaType(map[string]string{
        dialect.Postgres: "STRUCT(street VARCHAR, city VARCHAR, zip VARCHAR)",
    })

// HUGEINT (128-bit integer)
field.Other("big_num", new(big.Int)).
    SchemaType(map[string]string{
        dialect.Postgres: "HUGEINT",
    })

// TIMESTAMPTZ
field.Time("created_at").
    SchemaType(map[string]string{
        dialect.Postgres: "TIMESTAMPTZ",
    })
```

Available DuckDB types are documented in `ducktype/ducktype.go`.

---

## Transactions

```go
tx, err := drv.Tx(ctx)
if err != nil {
    log.Fatal(err)
}

if err := tx.Exec(ctx, `INSERT INTO users(name) VALUES($1)`, []any{"Bob"}, nil); err != nil {
    _ = tx.Rollback()
    log.Fatal(err)
}

if err := tx.Commit(); err != nil {
    log.Fatal(err)
}
```

For fine-grained control use `BeginTx`:

```go
tx, err := drv.BeginTx(ctx, &sql.TxOptions{
    Isolation: sql.LevelSerializable,
    ReadOnly:  false,
})
```

---

## Testing

In-memory databases are perfect for isolated unit tests:

```go
func newTestClient(t *testing.T) *ent.Client {
    t.Helper()
    drv := entduck.MustOpen(":memory:")
    t.Cleanup(func() { drv.Close() })

    client := ent.NewClient(ent.Driver(drv))
    if err := duckmigrate.NewSchema(drv, entmigrate.Tables).Create(context.Background()); err != nil {
        t.Fatalf("schema: %v", err)
    }
    return client
}
```

---

## Important Notes

### Dialect
`entduck` reports `dialect.Postgres` to ent for SQL generation. Do not use the generated `client.Schema.Create` for DuckDB migrations; it can emit PostgreSQL DDL that DuckDB does not implement. Use `github.com/lib-x/entduck/migrate` with the generated `ent/migrate.Tables`.

### DuckDB DDL Compatibility
The driver rewrites runtime SQL before execution:

| PostgreSQL SQL | DuckDB SQL |
| --- | --- |
| `jsonb` | `json` |
| `ON DELETE CASCADE` | `ON DELETE NO ACTION` |
| `ON DELETE SET NULL` | `ON DELETE NO ACTION` |
| `ON DELETE SET DEFAULT` | `ON DELETE NO ACTION` |

The migration package also normalizes generated ent table metadata before running DDL:

| Issue | Handling |
| --- | --- |
| `field.JSON` generates `jsonb` for postgres | Uses DuckDB `json` |
| ent integer IDs generate postgres identity columns | Creates DuckDB sequences and `DEFAULT nextval(...)` |
| DuckDB does not support `ALTER TABLE ... ADD/DROP CONSTRAINT` | Creates supported foreign keys inline during table creation and disables ent's later FK alter pass |
| DuckDB does not support `ON DELETE CASCADE/SET NULL/SET DEFAULT` | Downgrades to `NO ACTION`; cascading behavior must be implemented in application code |
| `DROP COLUMN` fails while a dependent index exists | The driver drops dependent indexes first, then drops the column |

Nested transactions and `SAVEPOINT` are not supported by DuckDB. Avoid ent patterns that rely on savepoints inside an existing transaction.

### In-Memory Connections
DuckDB in-memory databases are **per-connection**. If you open multiple connections to `":memory:"`, each gets a separate empty database. `entduck.Open(":memory:")` automatically enforces `MaxOpenConns(1)` to prevent this footgun.

### Write Concurrency
DuckDB (< v1.1) allows only **one write transaction at a time**. Design your application accordingly or use DuckDB's newer WAL mode for concurrent writes.

### File Locking
A DuckDB file database can only be opened by **one process** at a time. It is not suitable as a shared database server.

---

## License

MIT
