package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

type Config struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	RequestTimeout  time.Duration
}

type Client struct {
	db *sql.DB
}

func New(config Config) (*Client, error) {
	if config.DSN == "" {
		return nil, errors.New("MySQL DSN is required")
	}
	if config.MaxOpenConns <= 0 {
		return nil, errors.New("MySQL max open connections must be positive")
	}
	if config.MaxIdleConns < 0 || config.MaxIdleConns > config.MaxOpenConns {
		return nil, errors.New("MySQL max idle connections must be between zero and max open connections")
	}
	if config.ConnMaxLifetime <= 0 {
		return nil, errors.New("MySQL connection max lifetime must be positive")
	}
	if config.RequestTimeout <= 0 {
		return nil, errors.New("MySQL request timeout must be positive")
	}

	driverConfig, err := mysqldriver.ParseDSN(config.DSN)
	if err != nil {
		return nil, err
	}
	driverConfig.ParseTime = true
	if driverConfig.Timeout == 0 {
		driverConfig.Timeout = config.RequestTimeout
	}
	if driverConfig.ReadTimeout == 0 {
		driverConfig.ReadTimeout = config.RequestTimeout
	}
	if driverConfig.WriteTimeout == 0 {
		driverConfig.WriteTimeout = config.RequestTimeout
	}
	connector, err := mysqldriver.NewConnector(driverConfig)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
	return &Client{db: db}, nil
}

func (c *Client) DB() *sql.DB {
	return c.db
}

func (c *Client) Ping(ctx context.Context) error {
	return c.db.PingContext(ctx)
}

func (c *Client) EnsureSchema(ctx context.Context) error {
	for _, statement := range SchemaStatements {
		if _, err := c.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Close() error {
	return c.db.Close()
}
