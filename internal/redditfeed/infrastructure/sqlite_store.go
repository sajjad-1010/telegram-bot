package infrastructure

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"telegram-bot/internal/redditfeed/domain"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

func (s *SQLiteStore) Ensure(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("reddit feed db is nil")
	}

	stmts := []string{
		`
		CREATE TABLE IF NOT EXISTS reddit_feed_runs (
			subreddit TEXT NOT NULL,
			topic TEXT NOT NULL,
			last_run_at TEXT NOT NULL,
			PRIMARY KEY (subreddit, topic)
		)
		`,
		`
		CREATE TABLE IF NOT EXISTS reddit_feed_sent_posts (
			subreddit TEXT NOT NULL,
			topic TEXT NOT NULL,
			post_id TEXT NOT NULL,
			sent_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (subreddit, topic, post_id)
		)
		`,
		`CREATE INDEX IF NOT EXISTS idx_reddit_feed_sent_posts_sent_at ON reddit_feed_sent_posts(sent_at)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure reddit feed schema: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) LastRun(ctx context.Context, subreddit string, topic domain.Topic) (time.Time, bool, error) {
	if s.db == nil {
		return time.Time{}, false, fmt.Errorf("reddit feed db is nil")
	}

	var raw string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT last_run_at FROM reddit_feed_runs WHERE subreddit = ? AND topic = ?`,
		subreddit,
		topic.String(),
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query reddit feed last run: %w", err)
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse reddit feed last run: %w", err)
	}
	return parsed, true, nil
}

func (s *SQLiteStore) SetLastRun(ctx context.Context, subreddit string, topic domain.Topic, when time.Time) error {
	if s.db == nil {
		return fmt.Errorf("reddit feed db is nil")
	}

	_, err := s.db.ExecContext(
		ctx,
		`
		INSERT INTO reddit_feed_runs(subreddit, topic, last_run_at)
		VALUES(?, ?, ?)
		ON CONFLICT(subreddit, topic) DO UPDATE SET
			last_run_at = excluded.last_run_at
		`,
		subreddit,
		topic.String(),
		when.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert reddit feed last run: %w", err)
	}
	return nil
}

func (s *SQLiteStore) WasSent(ctx context.Context, subreddit string, topic domain.Topic, postID string) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("reddit feed db is nil")
	}

	var exists int
	err := s.db.QueryRowContext(
		ctx,
		`SELECT 1 FROM reddit_feed_sent_posts WHERE subreddit = ? AND topic = ? AND post_id = ?`,
		subreddit,
		topic.String(),
		postID,
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query reddit feed sent post: %w", err)
	}
	return exists == 1, nil
}

func (s *SQLiteStore) MarkSent(ctx context.Context, subreddit string, topic domain.Topic, postID string, sentAt time.Time) error {
	if s.db == nil {
		return fmt.Errorf("reddit feed db is nil")
	}

	_, err := s.db.ExecContext(
		ctx,
		`
		INSERT OR IGNORE INTO reddit_feed_sent_posts(subreddit, topic, post_id, sent_at)
		VALUES(?, ?, ?, ?)
		`,
		subreddit,
		topic.String(),
		postID,
		sentAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert reddit feed sent post: %w", err)
	}
	return nil
}
