// Package db owns the Postgres connection and schema for this backend's
// own state (business accounts, sessions, business content configuration).
// Every internal/* database-access package goes through GORM; there is no
// raw database/sql handle exposed here.
package db

import (
	"context"
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
	if err := applySchema(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
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

// schemaLockKey is an arbitrary application-wide Postgres advisory lock id.
const schemaLockKey int64 = 0x61695f737570706f // "ai_suppo"

// applySchema runs schema.sql under an advisory lock. The statements are
// idempotent, but ALTER TABLE takes strong table locks, so two processes
// applying the schema at the same moment (several instances starting
// together, or test packages running in parallel) can deadlock each other.
// The lock makes them take turns. It is session-scoped, so it is taken and
// released on one dedicated connection.
func applySchema(sqlDB *sql.DB) error {
	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("db: schema connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", schemaLockKey); err != nil {
		return fmt.Errorf("db: take schema lock: %w", err)
	}
	defer conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", schemaLockKey)
	if _, err := conn.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("db: apply schema: %w", err)
	}
	return nil
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
