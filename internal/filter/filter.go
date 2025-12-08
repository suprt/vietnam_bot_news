package filter

import (
	"context"
	"strings"
	"time"

	"github.com/maine/vietnam_bot_news/internal/config"
	"github.com/maine/vietnam_bot_news/internal/news"
)

// Filter реализует бизнес-правила отсечения новостей (docs/architecture.md).
type Filter struct {
	cfg config.Pipeline
}

// New создаёт экземпляр фильтра.
func New(cfg config.Pipeline) *Filter {
	return &Filter{cfg: cfg}
}

// Apply реализует app.Filter.
func (f *Filter) Apply(ctx context.Context, articles []news.ArticleRaw, state news.State) ([]news.ArticleRaw, error) {
	_ = ctx // на MVP фильтр не использует контекст

	sentIDs := make(map[string]struct{}, len(state.SentArticles))
	for _, item := range state.SentArticles {
		sentIDs[item.ID] = struct{}{}
	}

	now := time.Now()
	cutoff := now.Add(-time.Duration(f.cfg.RecencyMaxHours) * time.Hour)

	seen := make(map[string]struct{})
	filtered := make([]news.ArticleRaw, 0, len(articles))

	for _, article := range articles {
		// Фильтруем старые статьи
		if article.PublishedAt.Before(cutoff) {
			continue
		}

		// Фильтруем статьи с датой в будущем (некорректные даты в RSS)
		if article.PublishedAt.After(now) {
			continue
		}

		if len([]rune(strings.TrimSpace(article.RawContent))) < f.cfg.MinContentLength {
			continue
		}

		key := canonicalKey(article)
		if _, ok := seen[key]; ok {
			continue
		}

		if _, alreadySent := sentIDs[article.ID]; alreadySent {
			continue
		}

		seen[key] = struct{}{}
		filtered = append(filtered, article)
	}

	return filtered, nil
}

func canonicalKey(article news.ArticleRaw) string {
	base := strings.ToLower(strings.TrimSpace(article.URL))
	if base == "" {
		base = strings.ToLower(strings.TrimSpace(article.Title))
	}
	return base
}


