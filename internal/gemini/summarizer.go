package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/maine/vietnam_bot_news/internal/config"
	"github.com/maine/vietnam_bot_news/internal/news"
)

// Summarizer реализует app.Summarizer, используя Gemini API для создания кратких резюме новостей.
type Summarizer struct {
	client    GeminiClient
	cfg       config.Gemini
	batchSize int
}

// NewSummarizer создаёт новый экземпляр суммаризатора.
func NewSummarizer(client GeminiClient, geminiCfg config.Gemini) *Summarizer {
	batchSize := geminiCfg.BatchSizeSummary
	if batchSize <= 0 {
		batchSize = 5 // дефолтное значение
	}
	return &Summarizer{
		client:    client,
		cfg:       geminiCfg,
		batchSize: batchSize,
	}
}

// Summarize реализует app.Summarizer.
func (s *Summarizer) Summarize(ctx context.Context, articles []news.CategorizedArticle) ([]news.DigestEntry, error) {
	if len(articles) == 0 {
		return nil, nil
	}

	var results []news.DigestEntry
	articleMap := make(map[string]news.CategorizedArticle, len(articles))
	for _, article := range articles {
		articleMap[article.Article.ID] = article
	}

	// Разбиваем на батчи
	for i := 0; i < len(articles); i += s.batchSize {
		end := i + s.batchSize
		if end > len(articles) {
			end = len(articles)
		}

		batch := articles[i:end]
		batchResults, err := s.summarizeBatch(ctx, batch, articleMap)
		if err != nil {
			return nil, fmt.Errorf("summarize batch [%d-%d]: %w", i, end-1, err)
		}

		results = append(results, batchResults...)
	}

	return results, nil
}

func (s *Summarizer) summarizeBatch(ctx context.Context, articles []news.CategorizedArticle, articleMap map[string]news.CategorizedArticle) ([]news.DigestEntry, error) {
	// Формируем входные данные для промпта
	inputData := make([]articleInput, 0, len(articles))
	for _, catArticle := range articles {
		inputData = append(inputData, articleInput{
			ID:      catArticle.Article.ID,
			Title:   catArticle.Article.Title,
			Content: catArticle.Article.RawContent,
		})
	}

	inputJSON, err := json.Marshal(inputData)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	// Формируем промпт согласно docs/prompting.md
	prompt := s.buildPrompt(string(inputJSON))

	// Вызываем Gemini API
	responseText, err := s.client.GenerateText(ctx, s.cfg.ModelSummary, prompt)
	if err != nil {
		return nil, fmt.Errorf("generate text: %w", err)
	}

	// Парсим JSON ответ
	var summaries []summaryResponse
	if err := json.Unmarshal([]byte(responseText), &summaries); err != nil {
		// Пытаемся извлечь JSON из текста, если модель добавила лишнее
		cleaned := extractJSON(responseText)
		if cleaned == "" {
			return nil, fmt.Errorf("unmarshal response: %w (raw: %s)", err, responseText)
		}
		if err := json.Unmarshal([]byte(cleaned), &summaries); err != nil {
			return nil, fmt.Errorf("unmarshal cleaned response: %w (raw: %s)", err, responseText)
		}
	}

	// Формируем результат - преобразуем CategorizedArticle → DigestEntry
	// Сначала обрабатываем ответы от Gemini
	summariesMap := make(map[string]string, len(summaries))
	for _, summaryResp := range summaries {
		summaryRU := strings.TrimSpace(summaryResp.SummaryRU)
		if summaryRU != "" {
			summariesMap[summaryResp.ID] = summaryRU
		}
	}

	// Формируем результаты для всех статей
	results := make([]news.DigestEntry, 0, len(articles))
	for _, catArticle := range articles {
		summaryRU, ok := summariesMap[catArticle.Article.ID]
		if !ok || summaryRU == "" {
			// Fallback: если Gemini не вернул summary, используем заголовок
			summaryRU = catArticle.Article.Title
		}

		results = append(results, news.DigestEntry{
			ID:          catArticle.Article.ID,
			Category:    catArticle.Category,
			Title:       catArticle.Article.Title,
			URL:         catArticle.Article.URL,
			SummaryRU:   summaryRU,
			Source:      catArticle.Article.Source,
			PublishedAt: catArticle.Article.PublishedAt,
		})
	}

	return results, nil
}

func (s *Summarizer) buildPrompt(inputJSON string) string {
	return fmt.Sprintf(`Ты — русскоязычный редактор новостной ленты.
Тебе будет передан список новостей с уникальными идентификаторами id, заголовками и полным текстом на вьетнамском (иногда на английском).
Для каждой новости сделай краткое резюме на русском языке длиной 1–2 предложения.
Используй нейтральный, информативный стиль, без оценочных суждений и кликовбейта. Не придумывай факты, которых нет в тексте.
Верни результат в виде списка объектов JSON без дополнительных комментариев. Формат:
[{"id": "<id новости>", "summary_ru": "<краткое резюме на русском>"}, ...]

Входные данные:
%s`, inputJSON)
}

type summaryResponse struct {
	ID        string `json:"id"`
	SummaryRU string `json:"summary_ru"`
}

