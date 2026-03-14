# oracle11gdriver

`oracle11gdriver` is a thin `database/sql` wrapper around [`go-ora`](https://github.com/sijms/go-ora) for the GraphJin Oracle 11g path.

It exists because Oracle 11g does not support the JSON SQL features used by the regular GraphJin Oracle dialect (`JSON_OBJECT`, `JSON_ARRAYAGG`, and related 12c+ functionality).

Instead of asking Oracle 11g to build nested JSON in SQL, GraphJin:

1. Compiles a GraphQL query into a JSON instruction set.
2. Sends that instruction set to this driver as the "query string".
3. Lets the driver execute multiple plain `SELECT` statements through `go-ora`.
4. Assembles the final nested JSON response in Go.

For normal SQL statements, the driver simply passes everything through to `go-ora`.

## Why wrap the driver?

GraphJin's execution engine expects a database query to eventually return a single JSON payload.

That works naturally on databases that can build JSON in SQL, but Oracle 11g cannot.

Without this wrapper, supporting Oracle 11g would require a much more invasive change to GraphJin's core execution path:

- the dialect would need to execute multiple queries itself
- the core engine would need special-case execution logic for Oracle 11g
- the standard `db.QueryContext(...)` flow would no longer be enough

Wrapping the driver keeps the Oracle 11g workaround isolated at the boundary:

- `qcode` stays unchanged
- GraphJin core execution stays mostly unchanged
- schema discovery still uses normal SQL
- low-level Oracle connectivity is still handled by `go-ora`

In short: this package is not a replacement Oracle driver, it is a GraphJin-specific execution adapter on top of `go-ora`.

## What it does

When the incoming query is a JSON object, the driver treats it as a GraphJin Oracle 11g instruction set.

The driver then:

- parses the instruction set
- finds root queries and child queries
- resolves GraphQL variables into Oracle bind arguments
- resolves child query bind values from parent rows
- executes the generated SQL statements
- normalizes Oracle values into JSON-friendly Go values
- attaches child results into parent objects
- returns a single-row, single-column JSON response (`__root`)

When the incoming query is plain SQL, the driver:

- forwards it directly to the wrapped `go-ora` connection

That passthrough behavior is what allows GraphJin schema discovery and other normal database operations to continue working.

## Instruction model

The driver currently expects an instruction payload with:

- `operation: "oracle11g_query"`
- a flat `queries` array
- per-query metadata such as:
  - `id`
  - `field_name`
  - `sql`
  - `params`
  - `columns`
  - `children`
  - `singular`

Parameter sources are one of:

- `arg`: value comes from the original GraphQL variable list
- `parent`: value comes from a previously fetched parent row

## Current scope

This package is intentionally narrow.

Supported well:

- read-only query execution
- nested parent/child GraphQL reads
- Oracle 11g-compatible plain `SELECT` execution
- JSON assembly in Go

Not the target of this package right now:

- mutations
- subscriptions
- Oracle 12c+ JSON SQL features
- a general-purpose Oracle abstraction outside GraphJin

## Usage

Register the driver with a blank import and open the database with `oracle11g`:

```go
import (
    "database/sql"

    _ "github.com/dosco/graphjin/oracle11gdriver"
)

db, err := sql.Open("oracle11g", "oracle://user:password@host:1521/xe")
if err != nil {
    panic(err)
}
```

Then configure GraphJin with:

```go
conf := &core.Config{
    DBType:           "oracle11g",
    DisableAllowList: true,
}
```

The working example lives in `examples/oracle-poc`.

## Files

- `driver.go`: wraps `go-ora` and registers the `oracle11g` driver name
- `conn.go`: intercepts instruction JSON and passes through normal SQL
- `executor.go`: executes query plans and assembles nested JSON
- `rows.go`: exposes the final JSON payload as a single-row `driver.Rows`

## Design intent

The package is intentionally pragmatic.

It exists to make Oracle 11g usable without polluting the rest of GraphJin with Oracle 11g-specific execution rules.
