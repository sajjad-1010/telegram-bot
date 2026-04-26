package domain

import "strings"

type Topic string

const (
	TopicLatest Topic = "latest"
	TopicTop    Topic = "top"
	TopicHot    Topic = "hot"
)

func AllTopics() []Topic {
	return []Topic{TopicLatest, TopicTop, TopicHot}
}

func (t Topic) String() string {
	return string(t)
}

func (t Topic) IsValid() bool {
	switch t {
	case TopicLatest, TopicTop, TopicHot:
		return true
	default:
		return false
	}
}

type Post struct {
	ID               string
	Title            string
	SelfText         string
	Permalink        string
	URL              string
	Subreddit        string
	NativeMedia      bool
	ExternalMedia    bool
	ExternalMediaURL string
	DownloadURL      string
	Stickied         bool
}

func (p Post) Caption() string {
	return strings.TrimSpace(p.Title)
}

func (p Post) TextBody() string {
	title := strings.TrimSpace(p.Title)
	body := strings.TrimSpace(p.SelfText)
	link := strings.TrimSpace(p.Permalink)

	parts := make([]string, 0, 3)
	if title != "" {
		parts = append(parts, title)
	}
	if body != "" {
		parts = append(parts, body)
	}
	if link != "" {
		parts = append(parts, link)
	}
	return strings.Join(parts, "\n\n")
}

type Config struct {
	Enabled       bool
	Subreddit     string
	ChatID        int64
	IntervalHours int
	FetchLimit    int
	PostsPerTopic int
	TopWindow     string
	PollMinutes   int
}
