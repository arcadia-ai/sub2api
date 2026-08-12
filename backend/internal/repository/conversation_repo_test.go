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
