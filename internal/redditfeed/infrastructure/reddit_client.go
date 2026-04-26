package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"telegram-bot/internal/redditfeed/domain"
)

type RedditClient struct {
	httpClient *http.Client
	userAgent  string
}

func NewRedditClient(userAgent string) *RedditClient {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		userAgent = "telegram-bot/1.0"
	}

	return &RedditClient{
		httpClient: &http.Client{Timeout: 20 * time.Second},
		userAgent:  userAgent,
	}
}

func (c *RedditClient) FetchPosts(ctx context.Context, subreddit string, topic domain.Topic, limit int, topWindow string) ([]domain.Post, error) {
	endpoint, err := c.buildEndpoint(subreddit, topic, limit, topWindow)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build reddit request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reddit request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("reddit request failed with status %s", resp.Status)
	}

	var payload redditListing
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode reddit response: %w", err)
	}

	posts := make([]domain.Post, 0, len(payload.Data.Children))
	for _, child := range payload.Data.Children {
		post := mapRedditPost(child.Data)
		if post.ID == "" || post.Stickied {
			continue
		}
		posts = append(posts, post)
	}

	return posts, nil
}

func (c *RedditClient) buildEndpoint(subreddit string, topic domain.Topic, limit int, topWindow string) (string, error) {
	subreddit = strings.TrimSpace(strings.TrimPrefix(subreddit, "r/"))
	if subreddit == "" {
		return "", fmt.Errorf("subreddit is empty")
	}
	if !topic.IsValid() {
		return "", fmt.Errorf("invalid reddit feed topic: %s", topic)
	}
	if limit <= 0 {
		limit = 25
	}

	u := url.URL{
		Scheme: "https",
		Host:   "www.reddit.com",
		Path:   "/" + path.Join("r", subreddit, topicPath(topic)) + ".json",
	}

	q := u.Query()
	q.Set("raw_json", "1")
	q.Set("limit", fmt.Sprintf("%d", limit))
	if topic == domain.TopicTop {
		topWindow = strings.TrimSpace(strings.ToLower(topWindow))
		if topWindow == "" {
			topWindow = "day"
		}
		q.Set("t", topWindow)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func topicPath(topic domain.Topic) string {
	switch topic {
	case domain.TopicLatest:
		return "new"
	case domain.TopicTop:
		return "top"
	case domain.TopicHot:
		return "hot"
	default:
		return "new"
	}
}

type redditListing struct {
	Data struct {
		Children []struct {
			Data redditPost `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

type redditPost struct {
	ID                  string         `json:"id"`
	Title               string         `json:"title"`
	SelfText            string         `json:"selftext"`
	Permalink           string         `json:"permalink"`
	URL                 string         `json:"url"`
	URLOverriddenByDest string         `json:"url_overridden_by_dest"`
	Subreddit           string         `json:"subreddit"`
	Stickied            bool           `json:"stickied"`
	IsGallery           bool           `json:"is_gallery"`
	IsVideo             bool           `json:"is_video"`
	Media               map[string]any `json:"media"`
	SecureMedia         map[string]any `json:"secure_media"`
	Preview             map[string]any `json:"preview"`
	PostHint            string         `json:"post_hint"`
}

func mapRedditPost(raw redditPost) domain.Post {
	permalink := strings.TrimSpace(raw.Permalink)
	if permalink != "" && !strings.HasPrefix(permalink, "http") {
		permalink = "https://www.reddit.com" + permalink
	}

	postURL := strings.TrimSpace(raw.URLOverriddenByDest)
	if postURL == "" {
		postURL = strings.TrimSpace(raw.URL)
	}

	post := domain.Post{
		ID:        strings.TrimSpace(raw.ID),
		Title:     strings.TrimSpace(raw.Title),
		SelfText:  strings.TrimSpace(raw.SelfText),
		Permalink: permalink,
		URL:       postURL,
		Subreddit: strings.TrimSpace(raw.Subreddit),
		Stickied:  raw.Stickied,
	}

	if isExternalURL(postURL) {
		post.ExternalMedia = true
		post.ExternalMediaURL = postURL
		return post
	}

	if isRedditNativePost(raw, postURL) {
		post.NativeMedia = true
		post.DownloadURL = permalink
		return post
	}

	if postURL != "" {
		post.ExternalMedia = true
		post.ExternalMediaURL = postURL
	}
	return post
}

func isExternalURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "www.reddit.com", "reddit.com", "i.redd.it", "v.redd.it", "preview.redd.it", "external-preview.redd.it", "redd.it":
		return false
	default:
		return host != ""
	}
}

func isRedditNativePost(raw redditPost, postURL string) bool {
	if raw.IsGallery || raw.IsVideo {
		return true
	}
	if hasRedditVideo(raw.Media) || hasRedditVideo(raw.SecureMedia) || hasRedditVideoPreview(raw.Preview) {
		return true
	}

	host := ""
	if parsed, err := url.Parse(postURL); err == nil {
		host = strings.ToLower(parsed.Hostname())
	}

	switch host {
	case "i.redd.it", "v.redd.it", "preview.redd.it", "external-preview.redd.it", "www.reddit.com", "reddit.com":
		return true
	default:
		return false
	}
}

func hasRedditVideo(media map[string]any) bool {
	if media == nil {
		return false
	}
	if _, ok := media["reddit_video"]; ok {
		return true
	}
	return false
}

func hasRedditVideoPreview(preview map[string]any) bool {
	if preview == nil {
		return false
	}
	if _, ok := preview["reddit_video_preview"]; ok {
		return true
	}
	return false
}
