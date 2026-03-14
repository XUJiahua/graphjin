package oracle11gdriver

import (
	"database/sql/driver"
	"io"
)

type SingleValueRows struct {
	value    []byte
	columns  []string
	consumed bool
}

func NewSingleValueRows(value []byte, columns []string) *SingleValueRows {
	return &SingleValueRows{
		value:   value,
		columns: columns,
	}
}

func (r *SingleValueRows) Columns() []string {
	return r.columns
}

func (r *SingleValueRows) Close() error {
	return nil
}

func (r *SingleValueRows) Next(dest []driver.Value) error {
	if r.consumed {
		return io.EOF
	}
	r.consumed = true
	if len(dest) != 0 {
		dest[0] = r.value
	}
	return nil
}
