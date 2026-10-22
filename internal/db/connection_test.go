package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"instalbloc/internal/db/sqlc"
)

func TestOpenAndQueries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbFile := filepath.Join(t.TempDir(), "test_instalbloc.db")
	conn, state, err := Open(ctx, dbFile)
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	if state.JournalMode != "wal" {
		t.Errorf("expected journal_mode=wal, got %q", state.JournalMode)
	}
	if state.BusyTimeout < 5000 {
		t.Errorf("expected busy_timeout>=5000, got %d", state.BusyTimeout)
	}
	if state.ForeignKeys != 1 {
		t.Errorf("expected foreign_keys=1, got %d", state.ForeignKeys)
	}

	queries := sqlc.New(conn)
	now := time.Now().Unix()

	user, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        "test.locatar@instalbloc.ro",
		PasswordHash: sql.NullString{String: "pbkdf2$100000$aa$bb", Valid: true},
		Name:         "Ion Popescu",
		FirstName:    sql.NullString{String: "Ion", Valid: true},
		LastName:     sql.NullString{String: "Popescu", Valid: true},
		PrimaryRole:  "locatar",
		IsSubscribed: 1,
		IsVerified:   1,
		Phone:        sql.NullString{String: "0722000111", Valid: true},
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	fetched, err := queries.GetUserByEmail(ctx, "TEST.LOCATAR@instalbloc.ro")
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if fetched.ID != user.ID {
		t.Errorf("expected user ID %d, got %d", user.ID, fetched.ID)
	}
}
