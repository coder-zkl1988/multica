package handler

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type mockDB struct {
	db.DBTX
	getUserErr error
}

func (m *mockDB) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	return &mockRow{err: m.getUserErr}
}

func (m *mockDB) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 1"), nil
}

type mockRow struct {
	pgx.Row
	err error
}

func (m *mockRow) Scan(...interface{}) error { return m.err }
