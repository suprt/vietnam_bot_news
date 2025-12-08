package filter

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maine/vietnam_bot_news/internal/config"
	"github.com/maine/vietnam_bot_news/internal/news"
)

func TestFilter_Apply(t *testing.T) {
	now := time.Now()
	cfg := config.Pipeline{
		RecencyMaxHours:  48,
		MinContentLength: 100,
	}
	f := New(cfg)

	tests := []struct {
		name     string
		articles []news.ArticleRaw
		state    news.State
		want     int
	}{
		{
			name:     "empty input",
			articles: []news.ArticleRaw{},
			state:    news.State{},
			want:     0,
		},
		{
			name: "filter old articles",
			articles: []news.ArticleRaw{
				{
					ID:          "1",
					Title:       "Old news",
					URL:         "https://example.com/1",
					PublishedAt: now.Add(-50 * time.Hour),                                                                         // 50 hours ago
					RawContent:  strings.Repeat("A very long content that exceeds minimum length requirement for filtering. ", 2), // 100+ chars
				},
				{
					ID:          "2",
					Title:       "Recent news",
					URL:         "https://example.com/2",
					PublishedAt: now.Add(-24 * time.Hour),                                                                         // 24 hours ago
					RawContent:  strings.Repeat("A very long content that exceeds minimum length requirement for filtering. ", 2), // 100+ chars
				},
			},
			state: news.State{},
			want:  1, // Only recent news
		},
		{
			name: "filter short content",
			articles: []news.ArticleRaw{
				{
					ID:          "1",
					Title:       "Short",
					URL:         "https://example.com/1",
					PublishedAt: now,
					RawContent:  "Short", // Less than 100 chars
				},
				{
					ID:          "2",
					Title:       "Long content",
					URL:         "https://example.com/2",
					PublishedAt: now,
					RawContent:  strings.Repeat("This is a very long content that exceeds the minimum length requirement for filtering articles in the news pipeline system. ", 2), // 100+ chars
				},
			},
			state: news.State{},
			want:  1, // Only long content
		},
		{
			name: "filter already sent articles",
			articles: []news.ArticleRaw{
				{
					ID:          "already-sent",
					Title:       "Sent news",
					URL:         "https://example.com/sent",
					PublishedAt: now,
					RawContent:  strings.Repeat("A very long content that exceeds minimum length requirement for filtering. ", 2), // 100+ chars
				},
				{
					ID:          "new-article",
					Title:       "New news",
					URL:         "https://example.com/new",
					PublishedAt: now,
					RawContent:  strings.Repeat("A very long content that exceeds minimum length requirement for filtering. ", 2), // 100+ chars
				},
			},
			state: news.State{
				SentArticles: []news.StateArticle{
					{ID: "already-sent", SentAt: now.Add(-1 * time.Hour)},
				},
			},
			want: 1, // Only new article
		},
		{
			name: "filter duplicates by URL",
			articles: []news.ArticleRaw{
				{
					ID:          "1",
					Title:       "News",
					URL:         "https://example.com/same",
					PublishedAt: now,
					RawContent:  strings.Repeat("A very long content that exceeds minimum length requirement for filtering. ", 2), // 100+ chars
				},
				{
					ID:          "2",
					Title:       "Same news",
					URL:         "https://example.com/same", // Same URL
					PublishedAt: now,
					RawContent:  strings.Repeat("A very long content that exceeds minimum length requirement for filtering. ", 2), // 100+ chars
				},
			},
			state: news.State{},
			want:  1, // Only first occurrence
		},
		{
			name: "filter future dates",
			articles: []news.ArticleRaw{
				{
					ID:          "1",
					Title:       "Future news",
					URL:         "https://example.com/1",
					PublishedAt: now.Add(24 * time.Hour),                                                                          // Future date
					RawContent:  strings.Repeat("A very long content that exceeds minimum length requirement for filtering. ", 2), // 100+ chars
				},
				{
					ID:          "2",
					Title:       "Current news",
					URL:         "https://example.com/2",
					PublishedAt: now,
					RawContent:  strings.Repeat("A very long content that exceeds minimum length requirement for filtering. ", 2), // 100+ chars
				},
			},
			state: news.State{},
			want:  1, // Only current news
		},
		{
			name: "all filters combined",
			articles: []news.ArticleRaw{
				{
					ID:          "old",
					Title:       "Old",
					URL:         "https://example.com/old",
					PublishedAt: now.Add(-50 * time.Hour),
					RawContent:  strings.Repeat("Long enough content. ", 10), // 100+ chars
				},
				{
					ID:          "short",
					Title:       "Short",
					URL:         "https://example.com/short",
					PublishedAt: now,
					RawContent:  "Short", // Less than 100 chars
				},
				{
					ID:          "sent",
					Title:       "Sent",
					URL:         "https://example.com/sent",
					PublishedAt: now,
					RawContent:  strings.Repeat("A very long content that exceeds minimum length requirement for filtering. ", 2), // 100+ chars
				},
				{
					ID:          "future",
					Title:       "Future",
					URL:         "https://example.com/future",
					PublishedAt: now.Add(24 * time.Hour),
					RawContent:  strings.Repeat("A very long content that exceeds minimum length requirement for filtering. ", 2), // 100+ chars
				},
				{
					ID:          "valid",
					Title:       "Valid",
					URL:         "https://example.com/valid",
					PublishedAt: now.Add(-24 * time.Hour),
					RawContent:  strings.Repeat("A very long content that exceeds minimum length requirement for filtering. ", 2), // 100+ chars
				},
			},
			state: news.State{
				SentArticles: []news.StateArticle{
					{ID: "sent", SentAt: now.Add(-1 * time.Hour)},
				},
			},
			want: 1, // Only valid article
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			got, err := f.Apply(ctx, tt.articles, tt.state)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if len(got) != tt.want {
				t.Errorf("Apply() len = %v, want %v", len(got), tt.want)
			}
		})
	}
}

func TestFilter_canonicalKey(t *testing.T) {
	tests := []struct {
		name    string
		article news.ArticleRaw
		want    string
	}{
		{
			name: "with URL",
			article: news.ArticleRaw{
				URL:   "https://example.com/news",
				Title: "Title",
			},
			want: "https://example.com/news",
		},
		{
			name: "no URL, use title",
			article: news.ArticleRaw{
				URL:   "",
				Title: "Some Title",
			},
			want: "some title",
		},
		{
			name: "URL with spaces",
			article: news.ArticleRaw{
				URL:   "  https://example.com/news  ",
				Title: "Title",
			},
			want: "https://example.com/news",
		},
		{
			name: "case insensitive",
			article: news.ArticleRaw{
				URL:   "HTTPS://EXAMPLE.COM/NEWS",
				Title: "Title",
			},
			want: "https://example.com/news",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalKey(tt.article)
			if got != tt.want {
				t.Errorf("canonicalKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
