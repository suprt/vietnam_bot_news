package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/maine/vietnam_bot_news/internal/config"
	"github.com/maine/vietnam_bot_news/internal/news"
)

// Categorizer реализует app.Categorizer, используя Gemini API для категоризации новостей.
type Categorizer struct {
	client     GeminiClient
	cfg        config.Gemini
	categories []string
	batchSize  int
}

// NewCategorizer создаёт новый экземпляр категоризатора.
func NewCategorizer(client GeminiClient, geminiCfg config.Gemini, pipelineCfg config.Pipeline) *Categorizer {
	batchSize := geminiCfg.BatchSizeCategorization
	if batchSize <= 0 {
		batchSize = 15 // дефолтное значение
	}
	return &Categorizer{
		client:     client,
		cfg:        geminiCfg,
		categories: pipelineCfg.Categories,
		batchSize:  batchSize,
	}
}

// Categorize реализует app.Categorizer.
func (c *Categorizer) Categorize(ctx context.Context, articles []news.ArticleRaw) ([]news.CategorizedArticle, error) {
	if len(articles) == 0 {
		return nil, nil
	}

	var results []news.CategorizedArticle

	// Разбиваем на батчи
	for i := 0; i < len(articles); i += c.batchSize {
		end := i + c.batchSize
		if end > len(articles) {
			end = len(articles)
		}

		batch := articles[i:end]
		batchResults, err := c.categorizeBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("categorize batch [%d-%d]: %w", i, end-1, err)
		}

		results = append(results, batchResults...)
	}

	return results, nil
}

func (c *Categorizer) categorizeBatch(ctx context.Context, articles []news.ArticleRaw) ([]news.CategorizedArticle, error) {
	// Создаём map для быстрого поиска статьи по ID
	articleMap := make(map[string]news.ArticleRaw, len(articles))
	for _, article := range articles {
		articleMap[article.ID] = article
	}

	// Формируем входные данные для промпта
	inputData := make([]articleInput, 0, len(articles))
	for _, article := range articles {
		inputData = append(inputData, articleInput{
			ID:      article.ID,
			Title:   article.Title,
			Content: article.RawContent,
		})
	}

	inputJSON, err := json.Marshal(inputData)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	// Формируем промпт согласно docs/prompting.md
	prompt := c.buildPrompt(string(inputJSON))

	// Вызываем Gemini API
	responseText, err := c.client.GenerateText(ctx, c.cfg.ModelCategorization, prompt)
	if err != nil {
		return nil, fmt.Errorf("generate text: %w", err)
	}

	// Парсим JSON ответ
	var categories []categoryResponse
	if err := json.Unmarshal([]byte(responseText), &categories); err != nil {
		// Пытаемся извлечь JSON из текста, если модель добавила лишнее
		cleaned := extractJSON(responseText)
		if cleaned == "" {
			return nil, fmt.Errorf("unmarshal response: %w (raw: %s)", err, responseText)
		}
		if err := json.Unmarshal([]byte(cleaned), &categories); err != nil {
			return nil, fmt.Errorf("unmarshal cleaned response: %w (raw: %s)", err, responseText)
		}
	}

	// Формируем результат с валидацией
	// Сначала обрабатываем ответы от Gemini
	categorizedMap := make(map[string]news.CategorizedArticle, len(articles))
	for _, catResp := range categories {
		article, ok := articleMap[catResp.ID]
		if !ok {
			continue
		}

		category := strings.TrimSpace(catResp.Category)
		if !c.isValidCategory(category) {
			// Если категория невалидна, используем "Другое / Разное"
			category = "Другое / Разное"
		}

		categorizedMap[article.ID] = news.CategorizedArticle{
			Article:  article,
			Category: category,
		}
	}

	// Добавляем статьи, для которых Gemini не вернул категорию (fallback)
	for _, article := range articles {
		if _, ok := categorizedMap[article.ID]; !ok {
			// Fallback: если Gemini пропустил статью, используем дефолтную категорию
			categorizedMap[article.ID] = news.CategorizedArticle{
				Article:  article,
				Category: "Другое / Разное",
			}
		}
	}

	// Преобразуем map в slice, сохраняя порядок исходных статей
	results := make([]news.CategorizedArticle, 0, len(articles))
	for _, article := range articles {
		results = append(results, categorizedMap[article.ID])
	}

	return results, nil
}

func (c *Categorizer) buildPrompt(inputJSON string) string {
	categoriesList := strings.Join(c.categories, `", "`)

	return fmt.Sprintf(`Ты — помощник, который классифицирует новости по заданным категориям.
Тебе будет передан список новостей. Каждая новость имеет уникальный идентификатор id, заголовок и текст на вьетнамском языке (иногда на английском).
Твоя задача — для каждой новости выбрать ровно одну категорию из следующего списка:
"%s".
Верни результат в виде списка объектов JSON без дополнительных комментариев. Формат:
[{"id": "<id новости>", "category": "<одна категория из списка>"}, ...]

Входные данные:
%s`, categoriesList, inputJSON)
}

func (c *Categorizer) isValidCategory(category string) bool {
	for _, validCat := range c.categories {
		if strings.EqualFold(strings.TrimSpace(category), strings.TrimSpace(validCat)) {
			return true
		}
	}
	return false
}

func extractJSON(text string) string {
	// Ищем JSON-массив в тексте
	start := strings.Index(text, "[")
	if start == -1 {
		return ""
	}

	depth := 0
	for i := start; i < len(text); i++ {
		if text[i] == '[' {
			depth++
		} else if text[i] == ']' {
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}

	return ""
}

type articleInput struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type categoryResponse struct {
	ID       string `json:"id"`
	Category string `json:"category"`
}
