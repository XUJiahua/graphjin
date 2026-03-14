package oracle11gdriver

import (
	"context"
	"database/sql"
	"database/sql/driver"

	go_ora "github.com/sijms/go-ora/v2"
)

func init() {
	sql.Register("oracle11g", NewDriver())
}

type Driver struct {
	base *go_ora.OracleDriver
}

func NewDriver() *Driver {
	return &Driver{base: go_ora.NewDriver()}
}

func (d *Driver) Open(name string) (driver.Conn, error) {
	conn, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &Conn{base: conn}, nil
}

func (d *Driver) OpenConnector(name string) (driver.Connector, error) {
	connector, err := d.base.OpenConnector(name)
	if err != nil {
		return nil, err
	}
	return &Connector{
		driver: d,
		base:   connector,
	}, nil
}

type Connector struct {
	driver *Driver
	base   driver.Connector
}

func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &Conn{base: conn}, nil
}

func (c *Connector) Driver() driver.Driver {
	return c.driver
}
