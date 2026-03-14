package oracle11gdriver

import (
	"context"
	"database/sql/driver"
	"strings"
)

type Conn struct {
	base driver.Conn
}

func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	return &Stmt{
		conn:  c,
		query: query,
	}, nil
}

func (c *Conn) Close() error {
	return c.base.Close()
}

func (c *Conn) Begin() (driver.Tx, error) {
	return c.base.Begin()
}

func (c *Conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if conn, ok := c.base.(driver.ConnBeginTx); ok {
		return conn.BeginTx(ctx, opts)
	}
	return c.Begin()
}

func (c *Conn) Ping(ctx context.Context) error {
	if pinger, ok := c.base.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

func (c *Conn) CheckNamedValue(nv *driver.NamedValue) error {
	if checker, ok := c.base.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(nv)
	}
	return nil
}

func (c *Conn) PrepareContext(_ context.Context, query string) (driver.Stmt, error) {
	return &Stmt{
		conn:  c,
		query: query,
	}, nil
}

func (c *Conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if isInstructionQuery(query) {
		return c.executeInstructions(ctx, query, args)
	}
	return c.queryBase(ctx, query, args)
}

func (c *Conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if isInstructionQuery(query) {
		rows, err := c.executeInstructions(ctx, query, args)
		if err != nil {
			return nil, err
		}
		_ = rows.Close()
		return driver.RowsAffected(0), nil
	}
	return c.execBase(ctx, query, args)
}

func (c *Conn) queryBase(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := c.base.(driver.QueryerContext); ok {
		return queryer.QueryContext(ctx, query, args)
	}

	stmt, err := c.base.Prepare(query)
	if err != nil {
		return nil, err
	}

	values := namedValuesToValues(args)
	rows, err := stmt.Query(values)
	if err != nil {
		_ = stmt.Close()
		return nil, err
	}
	return &stmtRows{
		Rows: rows,
		stmt: stmt,
	}, nil
}

func (c *Conn) execBase(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if execer, ok := c.base.(driver.ExecerContext); ok {
		return execer.ExecContext(ctx, query, args)
	}

	stmt, err := c.base.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close() //nolint:errcheck

	return stmt.Exec(namedValuesToValues(args))
}

func isInstructionQuery(query string) bool {
	trimmed := strings.TrimSpace(query)
	return strings.HasPrefix(trimmed, "{")
}

func namedValuesToValues(args []driver.NamedValue) []driver.Value {
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}
	return values
}

type stmtRows struct {
	driver.Rows
	stmt driver.Stmt
}

func (r *stmtRows) Close() error {
	closeErr := r.Rows.Close()
	stmtErr := r.stmt.Close()
	if closeErr != nil {
		return closeErr
	}
	return stmtErr
}

type Stmt struct {
	conn  *Conn
	query string
}

func (s *Stmt) Close() error {
	return nil
}

func (s *Stmt) NumInput() int {
	return -1
}

func (s *Stmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.conn.ExecContext(context.Background(), s.query, valuesToNamedValues(args))
}

func (s *Stmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.conn.QueryContext(context.Background(), s.query, valuesToNamedValues(args))
}

func (s *Stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.conn.ExecContext(ctx, s.query, args)
}

func (s *Stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.conn.QueryContext(ctx, s.query, args)
}

func valuesToNamedValues(args []driver.Value) []driver.NamedValue {
	named := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		named[i] = driver.NamedValue{
			Ordinal: i + 1,
			Value:   arg,
		}
	}
	return named
}
