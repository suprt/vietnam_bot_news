package news

import "time"

// ArticleRaw описывает новость сразу после получения из источника.
type ArticleRaw struct {
	ID          string            `json:"id"`
	Source      string            `json:"source"`
	Title       string            `json:"title"`
	URL         string            `json:"url"`
	PublishedAt time.Time         `json:"published_at"`
	RawLanguage string            `json:"raw_language"`
	RawContent  string            `json:"raw_content"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// CategorizedArticle содержит новость и категорию, присвоенную моделью.
type CategorizedArticle struct {
	Article            ArticleRaw `json:"article"`
	Category           string     `json:"category"`
	CategoryConfidence float64    `json:"category_confidence,omitempty"`
	RelevanceScore     float64    `json:"relevance_score,omitempty"` // Оценка актуальности от Gemini (0-10)
}

// DigestEntry — итоговое представление новости перед отправкой.
type DigestEntry struct {
	ID          string    `json:"id"`
	Category    string    `json:"category"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	SummaryRU   string    `json:"summary_ru"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"published_at"`
}

// State хранит минимальную информацию об уже отправленных новостях.
type State struct {
	LastRun      time.Time          `json:"last_run"`
	SentArticles []StateArticle     `json:"sent_articles"`
	Recipients   []RecipientBinding `json:"recipients"`
	Telegram     TelegramState      `json:"telegram"`
}

// StateArticle описывает запись об отправленной новости.
type StateArticle struct {
	ID     string    `json:"id"`
	SentAt time.Time `json:"sent_at"`
}

// RecipientBinding хранит известные чаты для рассылки.
type RecipientBinding struct {
	Name      string    `json:"name"`
	ChatID    string    `json:"chat_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TelegramState хранит служебную информацию для взаимодействия с Bot API.
type TelegramState struct {
	LastUpdateID int64 `json:"last_update_id"`
}
