package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestConversationSavePrefersRecentDuplicate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	item := &service.ConversationCapture{
		RequestUUID:    "47ccb639-615c-42d3-8cd6-6045700e954e",
		UserID:         9,
		APIKeyID:       12,
		Endpoint:       "/responses",
		RequestedModel: "gpt-test",
		HistoryHash:    "history-hash",
		StartedAt:      now,
		CompletedAt:    now,
		NormalizedRequest: []service.ConversationMessage{{Role: "user", Text: "same request"}},
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtextextended(")).
		WithArgs(item.UserID, item.APIKeyID, item.Endpoint, item.RequestedModel, `[{"role":"user","text":"same request"}]`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("AND r.request_fingerprint=md5($5::jsonb::text)")).
		WithArgs(item.UserID, item.APIKeyID, item.Endpoint, item.RequestedModel, `[{"role":"user","text":"same request"}]`, item.StartedAt).
		WillReturnRows(sqlmock.NewRows([]string{"session_id"}).AddRow(int64(42)))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE conversation_sessions SET last_model=$1,merge_source=$2,")).
		WithArgs(item.RequestedModel, "duplicate", int64(0), int64(0), item.CompletedAt, int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO conversation_requests")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(99)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO conversation_payloads")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := &ConversationRepository{db: db}
	require.NoError(t, repo.Save(context.Background(), item, nil, nil))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestConversationSaveRequiresOneHistoryCandidate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	item := &service.ConversationCapture{
		RequestUUID: "47ccb639-615c-42d3-8cd6-6045700e954e",
		UserID:      9,
		APIKeyID:    12,
		HistoryHash: "history-hash",
		StartedAt:   now,
		CompletedAt: now,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE (SELECT COUNT(*) FROM matches)=1`)).
		WithArgs(item.UserID, item.APIKeyID, item.HistoryHash).
		WillReturnError(sqlmock.ErrCancelled)
	mock.ExpectRollback()

	repo := &ConversationRepository{db: db}
	err = repo.Save(context.Background(), item, nil, nil)
	require.ErrorIs(t, err, sqlmock.ErrCancelled)
	require.NoError(t, mock.ExpectationsWereMet())
}
