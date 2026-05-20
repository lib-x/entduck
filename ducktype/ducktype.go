// Package ducktype provides DuckDB-specific column type definitions
// for use in ent schema field definitions.
//
// DuckDB supports a rich set of native types beyond what standard SQL offers.
// Use these type constants with ent's field.Other() or custom schema types
// to leverage DuckDB-native column types.
//
// # Example
//
//	func (User) Fields() []ent.Field {
//	    return []ent.Field{
//	        field.Other("tags", pgtype.TextArray{}).
//	            SchemaType(ducktype.ForDuck(ducktype.ListOf("VARCHAR"))),
//	        field.Float32("score").
//	            SchemaType(ducktype.ForDuck(ducktype.Float)),
//	    }
//	}
package ducktype

import "entgo.io/ent/dialect"

// SchemaMap is a convenience type alias for ent schema type maps.
// Use it with field.SchemaType() to specify DuckDB-native column types.
type SchemaMap = map[string]string

// DuckDB native column type constants.
// These can be used in ent field SchemaType declarations.
const (
	// Integer types
	TinyInt   = "TINYINT"   // 1-byte signed integer (-128 to 127)
	SmallInt  = "SMALLINT"  // 2-byte signed integer
	Integer   = "INTEGER"   // 4-byte signed integer
	BigInt    = "BIGINT"    // 8-byte signed integer
	HugeInt   = "HUGEINT"   // 16-byte signed integer
	UBigInt   = "UBIGINT"   // 8-byte unsigned integer
	UInteger  = "UINTEGER"  // 4-byte unsigned integer
	USmallInt = "USMALLINT" // 2-byte unsigned integer
	UTinyInt  = "UTINYINT"  // 1-byte unsigned integer (0 to 255)

	// Floating point types
	Float  = "FLOAT"  // 4-byte floating point (alias: REAL)
	Double = "DOUBLE" // 8-byte floating point (alias: DOUBLE PRECISION)

	// Decimal/numeric
	Decimal = "DECIMAL" // fixed-precision decimal (alias: NUMERIC)

	// String types
	Varchar = "VARCHAR" // variable-length string (alias: TEXT, STRING)
	Blob    = "BLOB"    // arbitrary binary data (alias: BYTEA, BINARY, VARBINARY)

	// Boolean
	Boolean = "BOOLEAN" // true/false (alias: BOOL, LOGICAL)

	// Date/time types
	Date        = "DATE"         // calendar date (year, month, day)
	Time        = "TIME"         // time of day (no time zone)
	TimeTZ      = "TIMETZ"       // time of day with time zone
	Timestamp   = "TIMESTAMP"    // date and time (no time zone)
	TimestampTZ = "TIMESTAMPTZ"  // date and time with time zone (alias: TIMESTAMP WITH TIME ZONE)
	TimestampS  = "TIMESTAMP_S"  // timestamp with second precision
	TimestampMS = "TIMESTAMP_MS" // timestamp with millisecond precision
	TimestampNS = "TIMESTAMP_NS" // timestamp with nanosecond precision
	Interval    = "INTERVAL"     // time span

	// UUID
	UUID = "UUID" // universally unique identifier (128-bit)

	// Complex / nested types
	List   = "LIST"   // ordered collection of values (usage: LIST(type))
	Struct = "STRUCT" // named fields with types (usage: STRUCT(k type, ...))
	Map    = "MAP"    // key-value pairs (usage: MAP(ktype, vtype))
	Union  = "UNION"  // tagged union of types (usage: UNION(k type, ...))

	// JSON
	JSON = "JSON" // arbitrary JSON data (stored as VARCHAR internally)

	// Bit string
	Bit = "BIT" // bit string of fixed or variable length

	// Enum (use EnumType helper)
	// Enum types must be created with CREATE TYPE ... AS ENUM before use.
)

// ListOf returns a DuckDB LIST column type for the given element type.
// Example: ducktype.ListOf("VARCHAR") → "VARCHAR[]"
func ListOf(elementType string) string {
	return elementType + "[]"
}

// MapOf returns a DuckDB MAP column type for the given key and value types.
// Example: ducktype.MapOf("VARCHAR", "INTEGER") → "MAP(VARCHAR, INTEGER)"
func MapOf(keyType, valueType string) string {
	return "MAP(" + keyType + ", " + valueType + ")"
}

// StructOf returns a DuckDB STRUCT column type.
// fields should be alternating name/type pairs.
// Example: ducktype.StructOf("name", "VARCHAR", "age", "INTEGER") → "STRUCT(name VARCHAR, age INTEGER)"
func StructOf(nameTypePairs ...string) string {
	if len(nameTypePairs)%2 != 0 {
		panic("ducktype.StructOf: arguments must be name/type pairs")
	}
	s := "STRUCT("
	for i := 0; i < len(nameTypePairs); i += 2 {
		if i > 0 {
			s += ", "
		}
		s += nameTypePairs[i] + " " + nameTypePairs[i+1]
	}
	return s + ")"
}

// ForDuck returns a SchemaMap that applies the given DuckDB column type
// when the schema dialect is postgres (as used by entduck).
//
//	field.String("tags").
//	    SchemaType(ducktype.ForDuck(ducktype.ListOf("VARCHAR")))
func ForDuck(columnType string) SchemaMap {
	return SchemaMap{
		dialect.Postgres: columnType,
	}
}
