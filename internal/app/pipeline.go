package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/maine/vietnam_bot_news/internal/config"
	"github.com/maine/vietnam_bot_news/internal/news"
)

const maxSentHistory = 500

// ErrNotConfigured возвращается, когда пайплайн запущен без обязательных зависимостей.
var ErrNotConfigured = errors.New("pipeline dependencies not configured")

// Clock определяет источник времени (удобно подменять в тестах).
type Clock func() time.Time

// SourceCollector агрегирует новости из подключённых источников.
type SourceCollector interface {
	Collect(ctx context.Context) ([]news.ArticleRaw, error)
}

// Filter отвечает за отсев старых, дублирующихся или неуместных новостей.
type Filter interface {
	Apply(ctx context.Context, articles []news.ArticleRaw, state news.State) ([]news.ArticleRaw, error)
}

// Categorizer распределяет новости по фиксированным категориям.
type Categorizer interface {
	Categorize(ctx context.Context, articles []news.ArticleRaw) ([]news.CategorizedArticle, error)
}

// Ranker сортирует и выбирает топ-N в каждой категории.
type Ranker interface {
	Rank(ctx context.Context, categorized []news.CategorizedArticle) ([]news.CategorizedArticle, error)
}

// Summarizer создаёт краткие русскоязычные summary.
type Summarizer interface {
	Summarize(ctx context.Context, articles []news.CategorizedArticle) ([]news.DigestEntry, error)
}

// Formatter превращает итоговые новости в Markdown-сообщения.
type Formatter interface {
	BuildMessages(entries []news.DigestEntry) ([]string, error)
}

// Sender публикует подготовленные сообщения в Telegram.
type Sender interface {
	Send(ctx context.Context, recipients []news.RecipientBinding, messages []string) error
}

// RecipientResolver управляет списком получателей.
type RecipientResolver interface {
	Resolve(ctx context.Context, state news.State) (news.State, []news.RecipientBinding, error)
}

// StateStore хранит и обновляет файл состояния.
type StateStore interface {
	Load(ctx context.Context) (news.State, error)
	Save(ctx context.Context, state news.State) error
}

// PipelineDeps перечисляет зависимости пайплайна.
type PipelineDeps struct {
	Collector       SourceCollector
	Filter          Filter
	Categorizer     Categorizer
	Ranker          Ranker
	Summarizer      Summarizer
	Formatter       Formatter
	Sender          Sender
	Recipients      RecipientResolver
	StateStore      StateStore
	Clock           Clock
	ForceDispatch   bool
	SkipGemini      bool
	SendTestMessage bool
	Config          config.Pipeline
}

// Pipeline инкапсулирует ежедневный процесс.
type Pipeline struct {
	collector       SourceCollector
	filter          Filter
	categorizer     Categorizer
	ranker          Ranker
	summarizer      Summarizer
	formatter       Formatter
	sender          Sender
	recipients      RecipientResolver
	stateStore      StateStore
	clock           Clock
	forceDispatch   bool
	skipGemini      bool
	sendTestMessage bool
	cfg             config.Pipeline
}

// NewPipeline создаёт новый экземпляр пайплайна.
func NewPipeline(deps PipelineDeps) *Pipeline {
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}

	return &Pipeline{
		collector:       deps.Collector,
		filter:          deps.Filter,
		categorizer:     deps.Categorizer,
		ranker:          deps.Ranker,
		summarizer:      deps.Summarizer,
		formatter:       deps.Formatter,
		sender:          deps.Sender,
		recipients:      deps.Recipients,
		stateStore:      deps.StateStore,
		clock:           clock,
		forceDispatch:   deps.ForceDispatch,
		skipGemini:      deps.SkipGemini,
		sendTestMessage: deps.SendTestMessage,
		cfg:             deps.Config,
	}
}

// Run исполняет полный цикл обработки новостей.
func (p *Pipeline) Run(ctx context.Context) error {
	if err := p.validateDeps(); err != nil {
		return err
	}

	state, err := p.stateStore.Load(ctx)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	var recipients []news.RecipientBinding
	if p.recipients != nil {
		state, recipients, err = p.recipients.Resolve(ctx, state)
		if err != nil {
			return fmt.Errorf("resolve recipients: %w", err)
		}
	}

	// Если установлен флаг отправки только тестового сообщения
	if p.sendTestMessage {
		log.Println("SEND_TEST_MESSAGE=1: Sending test message only (skipping all processing)")
		if len(recipients) > 0 && p.sender != nil {
			testMessage := "🧪 *Тестовое сообщение*\n\nЭто тестовое сообщение для проверки отправки в Telegram. Полный дайджест будет отправляться автоматически раз в день после обработки новостей через Gemini."
			log.Printf("Sending test message to %d recipient(s)...", len(recipients))
			if err := p.sender.Send(ctx, recipients, []string{testMessage}); err != nil {
				return fmt.Errorf("send test message: %w", err)
			}
			log.Println("Test message sent successfully")
		} else if len(recipients) == 0 {
			return fmt.Errorf("no recipients registered; ask users to contact the bot")
		}
		return nil
	}

	log.Println("Step 1: Collecting articles from RSS feeds...")
	rawArticles, err := p.collector.Collect(ctx)
	if err != nil {
		return fmt.Errorf("collect articles: %w", err)
	}
	log.Printf("Collected %d raw articles", len(rawArticles))

	log.Println("Step 2: Filtering articles...")
	filtered, err := p.filter.Apply(ctx, rawArticles, state)
	if err != nil {
		return fmt.Errorf("filter articles: %w", err)
	}
	log.Printf("After filtering: %d articles", len(filtered))

	// Оптимизация RPD: ограничиваем количество статей перед отправкой в Gemini
	// Берем только самые свежие статьи, чтобы не превысить лимит RPD=20
	// Это критично, так как даже с батчами 100, 1859 статей = ~19 запросов только на категоризацию
	if p.cfg.MaxArticlesBeforeGemini > 0 && len(filtered) > p.cfg.MaxArticlesBeforeGemini {
		// Сортируем по дате публикации (самые свежие первыми)
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].PublishedAt.After(filtered[j].PublishedAt)
		})
		originalCount := len(filtered)
		filtered = filtered[:p.cfg.MaxArticlesBeforeGemini]
		log.Printf("Limited articles from %d to %d (taking most recent) to optimize Gemini API usage (RPD limit)", originalCount, len(filtered))
	}

	// Детальная статистика по отобранным статьям
	log.Println("=== Article Selection Statistics ===")
	log.Printf("Total articles after filtering and limiting: %d", len(filtered))
	if len(filtered) > 0 {
		// Группируем по источникам
		sourceCount := make(map[string]int)
		for _, article := range filtered {
			sourceCount[article.Source]++
		}
		log.Println("Articles by source:")
		for source, count := range sourceCount {
			log.Printf("  - %s: %d articles", source, count)
		}
		// Показываем диапазон дат
		oldest := filtered[len(filtered)-1].PublishedAt
		newest := filtered[0].PublishedAt
		log.Printf("Date range: %s (oldest) to %s (newest)", oldest.Format("2006-01-02 15:04"), newest.Format("2006-01-02 15:04"))

		// Детальный список отобранных статей для отправки в Gemini
		log.Println("=== Selected Articles for Gemini Processing ===")
		for i, article := range filtered {
			// Ограничиваем длину заголовка для читаемости логов
			title := article.Title
			if len(title) > 80 {
				title = title[:80] + "..."
			}
			log.Printf("%3d. [%s] %s | %s | %s",
				i+1,
				article.Source,
				article.PublishedAt.Format("2006-01-02 15:04"),
				title,
				article.URL)
		}
		log.Println("=== End of Selected Articles ===")
	}

	// Если пропускаем Gemini, отправляем тестовое сообщение для проверки отправки
	if p.skipGemini {
		log.Println("SKIP_GEMINI=1: Skipping Gemini processing (categorization, ranking, summarization)")
		log.Println("Pipeline stopped after article selection (no API calls made)")

		// Отправляем тестовое сообщение для проверки отправки в Telegram
		if len(recipients) > 0 && p.sender != nil {
			testMessage := "🧪 *Тестовое сообщение*\n\nЭто тестовое сообщение для проверки отправки в Telegram. Полный дайджест будет отправляться автоматически раз в день после обработки новостей через Gemini."
			log.Printf("Sending test message to %d recipient(s)...", len(recipients))
			if err := p.sender.Send(ctx, recipients, []string{testMessage}); err != nil {
				log.Printf("Warning: failed to send test message: %v", err)
			} else {
				log.Println("Test message sent successfully")
			}
		} else if len(recipients) == 0 {
			log.Println("No recipients registered, skipping test message")
		}

		return nil
	}

	log.Println("Step 3: Categorizing articles with Gemini...")
	categorized, err := p.categorizer.Categorize(ctx, filtered)
	if err != nil {
		return fmt.Errorf("categorize articles: %w", err)
	}
	log.Printf("Categorized %d articles", len(categorized))

	log.Println("Step 4: Ranking articles with Gemini...")
	ranked, err := p.ranker.Rank(ctx, categorized)
	if err != nil {
		return fmt.Errorf("rank articles: %w", err)
	}
	log.Printf("Ranked: %d articles selected", len(ranked))

	log.Println("Step 5: Summarizing articles with Gemini...")
	digestEntries, err := p.summarizer.Summarize(ctx, ranked)
	if err != nil {
		return fmt.Errorf("summarize articles: %w", err)
	}
	log.Printf("Summarized %d articles", len(digestEntries))

	log.Println("=== Gemini API Usage Summary ===")
	log.Printf("Total articles processed: %d filtered -> %d categorized -> %d ranked -> %d summarized",
		len(filtered), len(categorized), len(ranked), len(digestEntries))
	log.Println("(Check individual step logs above for exact API request counts)")

	log.Println("Step 6: Formatting messages...")
	messages, err := p.formatter.BuildMessages(digestEntries)
	if err != nil {
		return fmt.Errorf("build messages: %w", err)
	}
	log.Printf("Formatted %d messages", len(messages))

	if len(messages) > 0 {
		if len(recipients) == 0 && !p.forceDispatch {
			return fmt.Errorf("no recipients registered; ask users to contact the bot")
		}
		if len(recipients) > 0 {
			if err := p.sender.Send(ctx, recipients, messages); err != nil {
				return fmt.Errorf("send messages: %w", err)
			}
		}
	}

	newState := p.updateState(state, digestEntries)
	if err := p.stateStore.Save(ctx, newState); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	return nil
}

func (p *Pipeline) validateDeps() error {
	// recipients опционален - он может быть nil, если auto_subscribe отключен
	// В этом случае pipeline будет работать только в режиме force_dispatch
	// Если skipGemini=true, то categorizer, ranker, summarizer, formatter, sender не обязательны
	switch {
	case p.collector == nil,
		p.filter == nil,
		p.stateStore == nil,
		p.clock == nil:
		return ErrNotConfigured
	}

	// Если не пропускаем Gemini, проверяем обязательные зависимости
	if !p.skipGemini {
		switch {
		case p.categorizer == nil,
			p.ranker == nil,
			p.summarizer == nil,
			p.formatter == nil,
			p.sender == nil:
			return ErrNotConfigured
		}
	}

	return nil
}

func (p *Pipeline) updateState(prev news.State, entries []news.DigestEntry) news.State {
	now := p.clock()
	prev.LastRun = now

	existing := make(map[string]struct{}, len(prev.SentArticles))
	filtered := make([]news.StateArticle, 0, len(prev.SentArticles))
	for _, item := range prev.SentArticles {
		existing[item.ID] = struct{}{}
		filtered = append(filtered, item)
	}

	for _, entry := range entries {
		if _, ok := existing[entry.ID]; ok {
			continue
		}
		filtered = append(filtered, news.StateArticle{
			ID:     entry.ID,
			SentAt: now,
		})
	}

	if len(filtered) > maxSentHistory {
		filtered = filtered[len(filtered)-maxSentHistory:]
	}

	prev.SentArticles = filtered
	return prev
}
