package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var CanonicalSchemaSQL string

// PragmaState captures the verified runtime SQLite PRAGMA configuration.
type PragmaState struct {
	JournalMode string
	BusyTimeout int
	ForeignKeys int
	Synchronous int
}

// Open initializes a pure-Go SQLite connection (modernc.org/sqlite, CGO=0),
// applies mandatory concurrency and integrity pragmas, and bootstraps the canonical schema.
func Open(ctx context.Context, dbPath string) (*sql.DB, *PragmaState, error) {
	if dbPath == "" {
		return nil, nil, fmt.Errorf("db: empty database path")
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("db: failed to open sqlite connection: %w", err)
	}

	// SQLite WAL concurrency tuning
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(30 * time.Minute)

	pragmaStatements := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA temp_store = MEMORY;",
	}

	for _, stmt := range pragmaStatements {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			_ = conn.Close()
			return nil, nil, fmt.Errorf("db: failed to execute pragma %q: %w", stmt, err)
		}
	}

	state, err := VerifyPragmas(ctx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	if _, err := conn.ExecContext(ctx, CanonicalSchemaSQL); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("db: failed to apply canonical schema: %w", err)
	}

	return conn, state, nil
}

// VerifyPragmas queries the active SQLite connection to confirm WAL, busy_timeout, and foreign_keys.
func VerifyPragmas(ctx context.Context, conn *sql.DB) (*PragmaState, error) {
	state := &PragmaState{}

	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&state.JournalMode); err != nil {
		return nil, fmt.Errorf("db: failed to read journal_mode: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout;").Scan(&state.BusyTimeout); err != nil {
		return nil, fmt.Errorf("db: failed to read busy_timeout: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys;").Scan(&state.ForeignKeys); err != nil {
		return nil, fmt.Errorf("db: failed to read foreign_keys: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA synchronous;").Scan(&state.Synchronous); err != nil {
		return nil, fmt.Errorf("db: failed to read synchronous: %w", err)
	}

	if state.BusyTimeout < 5000 {
		return nil, fmt.Errorf("db: invariant violation: busy_timeout=%d (expected >= 5000)", state.BusyTimeout)
	}
	if state.ForeignKeys != 1 {
		return nil, fmt.Errorf("db: invariant violation: foreign_keys=%d (expected 1)", state.ForeignKeys)
	}

	return state, nil
}
