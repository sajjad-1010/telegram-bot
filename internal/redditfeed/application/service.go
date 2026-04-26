package application

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"telegram-bot/internal/redditfeed/domain"
)

type Fetcher interface {
	FetchPosts(ctx context.Context, subreddit string, topic domain.Topic, limit int, topWindow string) ([]domain.Post, error)
}

type Store interface {
	Ensure(ctx context.Context) error
	LastRun(ctx context.Context, subreddit string, topic domain.Topic) (time.Time, bool, error)
	SetLastRun(ctx context.Context, subreddit string, topic domain.Topic, when time.Time) error
	WasSent(ctx context.Context, subreddit string, topic domain.Topic, postID string) (bool, error)
	MarkSent(ctx context.Context, subreddit string, topic domain.Topic, postID string, sentAt time.Time) error
}

type Publisher interface {
	Publish(ctx context.Context, topic domain.Topic, post domain.Post) error
}

type Service struct {
	cfg       domain.Config
	fetcher   Fetcher
	store     Store
	publisher Publisher
}

func NewService(cfg domain.Config, fetcher Fetcher, store Store, publisher Publisher) *Service {
	return &Service{
		cfg:       cfg,
		fetcher:   fetcher,
		store:     store,
		publisher: publisher,
	}
}

func (s *Service) StartBackground() error {
	if s == nil {
		return fmt.Errorf("reddit feed service is nil")
	}
	if !s.cfg.Enabled {
		log.Println("reddit feed scheduler disabled")
		return nil
	}
	if err := s.validateConfig(); err != nil {
		return err
	}

	ctx := context.Background()
	if err := s.store.Ensure(ctx); err != nil {
		return err
	}

	log.Printf(
		"reddit feed scheduler enabled subreddit=%s chat_id=%d interval_hours=%d poll_minutes=%d posts_per_topic=%d topics=%s,%s,%s",
		s.cfg.Subreddit,
		s.cfg.ChatID,
		s.cfg.IntervalHours,
		s.cfg.PollMinutes,
		s.cfg.PostsPerTopic,
		domain.TopicLatest,
		domain.TopicTop,
		domain.TopicHot,
	)

	go s.loop()
	return nil
}

func (s *Service) loop() {
	s.runDueTopics(context.Background())

	ticker := time.NewTicker(time.Duration(s.cfg.PollMinutes) * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.runDueTopics(context.Background())
	}
}

func (s *Service) runDueTopics(ctx context.Context) {
	for _, topic := range domain.AllTopics() {
		if !s.isTopicDue(ctx, topic) {
			continue
		}
		if err := s.runTopic(ctx, topic); err != nil {
			log.Printf("reddit feed topic run failed topic=%s subreddit=%s: %v", topic, s.cfg.Subreddit, err)
		}
	}
}

func (s *Service) isTopicDue(ctx context.Context, topic domain.Topic) bool {
	lastRun, ok, err := s.store.LastRun(ctx, s.cfg.Subreddit, topic)
	if err != nil {
		log.Printf("reddit feed read last run failed topic=%s: %v", topic, err)
		return false
	}
	if !ok {
		return true
	}
	return time.Since(lastRun.UTC()) >= time.Duration(s.cfg.IntervalHours)*time.Hour
}

func (s *Service) runTopic(ctx context.Context, topic domain.Topic) error {
	log.Printf("reddit feed topic start topic=%s subreddit=%s", topic, s.cfg.Subreddit)

	now := time.Now().UTC()
	posts, err := s.fetcher.FetchPosts(ctx, s.cfg.Subreddit, topic, s.cfg.FetchLimit, s.cfg.TopWindow)
	if err != nil {
		return fmt.Errorf("fetch posts: %w", err)
	}

	selectedPosts, err := s.pickNextPosts(ctx, topic, posts)
	if err != nil {
		return fmt.Errorf("pick posts: %w", err)
	}
	if len(selectedPosts) == 0 {
		log.Printf("reddit feed topic no new post topic=%s subreddit=%s", topic, s.cfg.Subreddit)
		return s.store.SetLastRun(ctx, s.cfg.Subreddit, topic, now)
	}

	for _, post := range selectedPosts {
		if err := s.publisher.Publish(ctx, topic, post); err != nil {
			return fmt.Errorf("publish post %s: %w", post.ID, err)
		}
		if err := s.store.MarkSent(ctx, s.cfg.Subreddit, topic, post.ID, now); err != nil {
			return fmt.Errorf("mark sent post %s: %w", post.ID, err)
		}
	}
	if err := s.store.SetLastRun(ctx, s.cfg.Subreddit, topic, now); err != nil {
		return fmt.Errorf("set last run: %w", err)
	}

	log.Printf("reddit feed topic success topic=%s subreddit=%s sent_posts=%d", topic, s.cfg.Subreddit, len(selectedPosts))
	return nil
}

func (s *Service) pickNextPosts(ctx context.Context, topic domain.Topic, posts []domain.Post) ([]domain.Post, error) {
	selected := make([]domain.Post, 0, s.cfg.PostsPerTopic)
	for _, post := range posts {
		if strings.TrimSpace(post.ID) == "" {
			continue
		}
		sent, err := s.store.WasSent(ctx, s.cfg.Subreddit, topic, post.ID)
		if err != nil {
			return nil, err
		}
		if sent {
			continue
		}
		selected = append(selected, post)
		if len(selected) >= s.cfg.PostsPerTopic {
			break
		}
	}
	return selected, nil
}

func (s *Service) validateConfig() error {
	if strings.TrimSpace(s.cfg.Subreddit) == "" {
		return fmt.Errorf("reddit feed subreddit is empty")
	}
	if s.cfg.ChatID == 0 {
		return fmt.Errorf("reddit feed chat id is empty")
	}
	if s.cfg.IntervalHours <= 0 {
		return fmt.Errorf("reddit feed interval must be positive")
	}
	if s.cfg.FetchLimit <= 0 {
		return fmt.Errorf("reddit feed fetch limit must be positive")
	}
	if s.cfg.PostsPerTopic <= 0 {
		return fmt.Errorf("reddit feed posts per topic must be positive")
	}
	if s.cfg.FetchLimit < s.cfg.PostsPerTopic {
		return fmt.Errorf("reddit feed fetch limit must be >= posts per topic")
	}
	if s.cfg.PollMinutes <= 0 {
		return fmt.Errorf("reddit feed poll minutes must be positive")
	}
	return nil
}
