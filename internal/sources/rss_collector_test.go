package sources

import (
	"context"
	"testing"
	"time"

	"github.com/maine/vietnam_bot_news/internal/config"
)

func TestRSSCollector_getRSSFeeds(t *testing.T) {
	tests := []struct {
		name string
		site config.Site
		want int
	}{
		{
			name: "new format with rss_feeds",
			site: config.Site{
				RSSFeeds: []string{"https://example.com/rss1", "https://example.com/rss2"},
			},
			want: 2,
		},
		{
			name: "old format with single RSS",
			site: config.Site{
				RSS: "https://example.com/rss",
			},
			want: 1,
		},
		{
			name: "priority to rss_feeds",
			site: config.Site{
				RSS:      "https://example.com/old",
				RSSFeeds: []string{"https://example.com/new"},
			},
			want: 1,
		},
		{
			name: "no RSS feeds",
			site: config.Site{},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := NewRSSCollector([]config.Site{tt.site}, nil, nil)
			feeds := collector.getRSSFeeds(tt.site)
			if len(feeds) != tt.want {
				t.Errorf("getRSSFeeds() len = %v, want %v", len(feeds), tt.want)
			}
		})
	}
}

func TestRSSCollector_parseTime(t *testing.T) {
	fallback := time.Date(2024, 12, 3, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		value    string
		expected bool // true if should parse successfully
	}{
		{
			name:     "RFC1123Z format",
			value:    "Mon, 02 Jan 2006 15:04:05 -0700",
			expected: true,
		},
		{
			name:     "RFC3339 format",
			value:    "2006-01-02T15:04:05-07:00",
			expected: true,
		},
		{
			name:     "empty string uses fallback",
			value:    "",
			expected: false, // uses fallback
		},
		{
			name:     "invalid format uses fallback",
			value:    "invalid date",
			expected: false, // uses fallback
		},
		{
			name:     "with spaces",
			value:    "  Mon, 02 Jan 2006 15:04:05 -0700  ",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTime(tt.value, fallback)
			if tt.expected {
				// Should parse to a different time than fallback
				if result.Equal(fallback) && tt.value != "" {
					t.Errorf("parseTime() should parse %q, but got fallback", tt.value)
				}
			} else {
				// Should use fallback
				if !result.Equal(fallback) {
					t.Errorf("parseTime() should use fallback, got %v", result)
				}
			}
		})
	}
}

func TestRSSCollector_buildArticleID(t *testing.T) {
	siteID := "test-site"
	url := "https://example.com/news"
	published := time.Date(2024, 12, 3, 12, 0, 0, 0, time.UTC)

	id1 := buildArticleID(siteID, url, published)
	id2 := buildArticleID(siteID, url, published)

	// Same inputs should produce same ID
	if id1 != id2 {
		t.Errorf("buildArticleID() should produce consistent IDs: %q != %q", id1, id2)
	}

	// Different URL should produce different ID
	id3 := buildArticleID(siteID, "https://example.com/other", published)
	if id1 == id3 {
		t.Errorf("buildArticleID() should produce different IDs for different URLs")
	}

	// Different timestamp should produce different ID
	id4 := buildArticleID(siteID, url, published.Add(time.Hour))
	if id1 == id4 {
		t.Errorf("buildArticleID() should produce different IDs for different timestamps")
	}

	// Should start with site ID
	if len(id1) == 0 || len(id1) < len(siteID) {
		t.Errorf("buildArticleID() should contain site ID")
	}
}

func TestRSSCollector_selectContent(t *testing.T) {
	tests := []struct {
		name     string
		item     rssItem
		expected string
	}{
		{
			name: "prefer content:encoded",
			item: rssItem{
				ContentEncoded: "Full content",
				Description:    "Description",
				Title:          "Title",
			},
			expected: "Full content",
		},
		{
			name: "use description if no content:encoded",
			item: rssItem{
				Description: "Description",
				Title:       "Title",
			},
			expected: "Description",
		},
		{
			name: "use title as fallback",
			item: rssItem{
				Title: "Title",
			},
			expected: "Title",
		},
		{
			name: "empty item returns empty title",
			item: rssItem{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := selectContent(tt.item)
			if result != tt.expected {
				t.Errorf("selectContent() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestRSSCollector_parseRSSFeed(t *testing.T) {
	validRSS := `<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <item>
      <title>Test Title</title>
      <link>https://example.com/1</link>
      <description>Test description</description>
      <pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate>
    </item>
    <item>
      <title>Test Title 2</title>
      <link>https://example.com/2</link>
      <description>Test description 2</description>
    </item>
  </channel>
</rss>`

	tests := []struct {
		name    string
		data    []byte
		wantErr bool
		wantLen int
	}{
		{
			name:    "valid RSS",
			data:    []byte(validRSS),
			wantErr: false,
			wantLen: 2,
		},
		{
			name:    "invalid XML",
			data:    []byte("not xml"),
			wantErr: true,
			wantLen: 0,
		},
		{
			name:    "empty RSS",
			data:    []byte(`<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`),
			wantErr: false,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := parseRSSFeed(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRSSFeed() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(items) != tt.wantLen {
				t.Errorf("parseRSSFeed() len = %v, want %v", len(items), tt.wantLen)
			}
		})
	}
}

func TestRSSCollector_Collect_EmptySites(t *testing.T) {
	collector := NewRSSCollector([]config.Site{}, nil, nil)
	ctx := context.Background()

	articles, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(articles) != 0 {
		t.Errorf("Collect() len = %v, want 0", len(articles))
	}
}

func TestRSSCollector_detectLanguage(t *testing.T) {
	site := config.Site{ID: "test"}
	lang := detectLanguage(site)
	if lang != "vi" {
		t.Errorf("detectLanguage() = %q, want %q", lang, "vi")
	}
}

