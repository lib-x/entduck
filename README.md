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
