package formatter

import (
	"strings"
	"testing"

	"github.com/maine/vietnam_bot_news/internal/config"
	"github.com/maine/vietnam_bot_news/internal/news"
)

func TestFormatter_BuildMessages(t *testing.T) {
	cfg := config.Pipeline{
		MaxTotalMessages: 5,
	}
	f := NewFormatter(cfg)

	tests := []struct {
		name    string
		entries []news.DigestEntry
		want    int // количество сообщений
	}{
		{
			name:    "empty entries",
			entries: []news.DigestEntry{},
			want:    0,
		},
		{
			name: "single category single message",
			entries: []news.DigestEntry{
				{
					ID:        "1",
					Category:  "Политика",
					Title:     "Новость 1",
					URL:       "https://example.com/1",
					SummaryRU: "Краткое содержание новости 1",
				},
				{
					ID:        "2",
					Category:  "Политика",
					Title:     "Новость 2",
					URL:       "https://example.com/2",
					SummaryRU: "Краткое содержание новости 2",
				},
			},
			want: 1,
		},
		{
			name: "multiple categories single message",
			entries: []news.DigestEntry{
				{
					ID:        "1",
					Category:  "Политика",
					Title:     "Новость 1",
					URL:       "https://example.com/1",
					SummaryRU: "Краткое содержание",
				},
				{
					ID:        "2",
					Category:  "Экономика и бизнес",
					Title:     "Новость 2",
					URL:       "https://example.com/2",
					SummaryRU: "Краткое содержание",
				},
			},
			want: 1,
		},
		{
			name: "empty category uses default",
			entries: []news.DigestEntry{
				{
					ID:        "1",
					Category:  "",
					Title:     "Новость",
					URL:       "https://example.com/1",
					SummaryRU: "Содержание",
				},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages, err := f.BuildMessages(tt.entries)
			if err != nil {
				t.Fatalf("BuildMessages() error = %v", err)
			}
			if len(messages) != tt.want {
				t.Errorf("BuildMessages() len = %v, want %v", len(messages), tt.want)
			}

			// Проверяем, что все сообщения не превышают лимит
			for i, msg := range messages {
				if len(msg) > telegramMaxMessageLength {
					t.Errorf("BuildMessages() message %d exceeds limit: %d > %d", i, len(msg), telegramMaxMessageLength)
				}
			}
		})
	}
}

func TestFormatter_BuildMessages_SplitLarge(t *testing.T) {
	cfg := config.Pipeline{
		MaxTotalMessages: 5,
	}
	f := NewFormatter(cfg)

	// Создаём очень длинные записи, которые не поместятся в одно сообщение
	entries := make([]news.DigestEntry, 0)
	longSummary := strings.Repeat("Очень длинное содержание новости. ", 50) // ~1500 символов на запись

	for i := 0; i < 5; i++ {
		entries = append(entries, news.DigestEntry{
			ID:        "article-" + string(rune('1'+i)),
			Category:  "Политика",
			Title:     "Новость " + string(rune('1'+i)),
			URL:       "https://example.com/" + string(rune('1'+i)),
			SummaryRU: longSummary,
		})
	}

	messages, err := f.BuildMessages(entries)
	if err != nil {
		t.Fatalf("BuildMessages() error = %v", err)
	}

	if len(messages) < 2 {
		t.Errorf("BuildMessages() should split into multiple messages, got %d", len(messages))
	}

	// Проверяем, что все сообщения не превышают лимит
	for i, msg := range messages {
		if len(msg) > telegramMaxMessageLength {
			t.Errorf("BuildMessages() message %d exceeds limit: %d > %d", i, len(msg), telegramMaxMessageLength)
		}
	}
}

func TestFormatter_BuildMessages_MarkdownFormat(t *testing.T) {
	cfg := config.Pipeline{
		MaxTotalMessages: 5,
	}
	f := NewFormatter(cfg)

	entries := []news.DigestEntry{
		{
			ID:        "1",
			Category:  "Политика",
			Title:     "Новость",
			URL:       "https://example.com/1",
			SummaryRU: "Краткое содержание",
		},
	}

	messages, err := f.BuildMessages(entries)
	if err != nil {
		t.Fatalf("BuildMessages() error = %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("BuildMessages() len = %v, want 1", len(messages))
	}

	msg := messages[0]
	if !strings.Contains(msg, "*Политика*") {
		t.Error("BuildMessages() should contain category header with markdown")
	}
	if !strings.Contains(msg, "[Новость](https://example.com/1)") {
		t.Error("BuildMessages() should contain title with link")
	}
	if !strings.Contains(msg, "— Краткое содержание") {
		t.Error("BuildMessages() should contain summary with separator")
	}
}

func TestFormatter_BuildMessages_Numbering(t *testing.T) {
	cfg := config.Pipeline{
		MaxTotalMessages: 5,
	}
	f := NewFormatter(cfg)

	// Создаём достаточно записей для разбиения на несколько сообщений
	longSummary := strings.Repeat("Длинное содержание. ", 100)
	entries := make([]news.DigestEntry, 0)
	for i := 0; i < 10; i++ {
		entries = append(entries, news.DigestEntry{
			ID:        "article-" + string(rune('1'+i)),
			Category:  "Политика",
			Title:     "Новость",
			URL:       "https://example.com/" + string(rune('1'+i)),
			SummaryRU: longSummary,
		})
	}

	messages, err := f.BuildMessages(entries)
	if err != nil {
		t.Fatalf("BuildMessages() error = %v", err)
	}

	if len(messages) > 1 {
		// Проверяем нумерацию только если сообщений больше одного
		for i, msg := range messages {
			if !strings.HasPrefix(msg, "Подборка дня") {
				t.Errorf("BuildMessages() message %d should have numbering header", i)
			}
			// Проверяем, что в сообщении есть номер (простая проверка)
			if !strings.Contains(msg, "(") || !strings.Contains(msg, "/") {
				t.Errorf("BuildMessages() message %d should contain numbering format", i)
			}
		}
	}
}
