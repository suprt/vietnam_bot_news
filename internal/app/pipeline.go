package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

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
	Collector     SourceCollector
	Filter        Filter
	Categorizer   Categorizer
	Ranker        Ranker
	Summarizer    Summarizer
	Formatter     Formatter
	Sender        Sender
	Recipients    RecipientResolver
	StateStore    StateStore
	Clock         Clock
	ForceDispatch bool
}

// Pipeline инкапсулирует ежедневный процесс.
type Pipeline struct {
	collector     SourceCollector
	filter        Filter
	categorizer   Categorizer
	ranker        Ranker
	summarizer    Summarizer
	formatter     Formatter
	sender        Sender
	recipients    RecipientResolver
	stateStore    StateStore
	clock         Clock
	forceDispatch bool
}

// NewPipeline создаёт новый экземпляр пайплайна.
func NewPipeline(deps PipelineDeps) *Pipeline {
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}

	return &Pipeline{
		collector:     deps.Collector,
		filter:        deps.Filter,
		categorizer:   deps.Categorizer,
		ranker:        deps.Ranker,
		summarizer:    deps.Summarizer,
		formatter:     deps.Formatter,
		sender:        deps.Sender,
		recipients:    deps.Recipients,
		stateStore:    deps.StateStore,
		clock:         clock,
		forceDispatch: deps.ForceDispatch,
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
	switch {
	case p.collector == nil,
		p.filter == nil,
		p.categorizer == nil,
		p.ranker == nil,
		p.summarizer == nil,
		p.formatter == nil,
		p.sender == nil,
		p.stateStore == nil,
		p.clock == nil:
		return ErrNotConfigured
	default:
		return nil
	}
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
