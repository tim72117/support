// Package db owns the Postgres connection and schema for this backend's
// own state (business accounts, sessions, business content configuration).
// Every internal/* database-access package goes through GORM; there is no
// raw database/sql handle exposed here.
package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

//go:embed schema.sql
var schemaSQL string

// Open connects to Postgres at dsn and applies schema.sql. Safe to call on
// every startup: every statement in schema.sql is idempotent (CREATE ... IF
// NOT EXISTS), so this never fails or duplicates state on a database that
// already has the schema from a previous run.
func Open(dsn string) (*gorm.DB, error) {
	sqlDB, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("db: apply schema: %w", err)
	}

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, DriverName: "postgres"}), &gorm.Config{
		Logger: newGormLogger(),
	})
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("db: gorm open: %w", err)
	}

	return gormDB, nil
}

// newGormLogger mirrors GORM's own default (Warn level, slow-query
// threshold), with gorm.ErrRecordNotFound silenced — every First()-based
// credential check (session.Login, ...) hits it on a routine, expected miss
// (wrong password, unregistered email), not a fault worth logging as an
// error.
func newGormLogger() gormlogger.Interface {
	return gormlogger.New(
		log.New(os.Stderr, "\r\n", log.LstdFlags),
		gormlogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
		},
	)
}
