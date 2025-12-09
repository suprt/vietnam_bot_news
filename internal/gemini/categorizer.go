package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

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
	var articlesForGemini []news.ArticleRaw

	// Разделяем статьи: с категорией из RSS и без категории
	for _, article := range articles {
		if category, ok := article.Metadata["rss_category"]; ok && category != "" {
			// Категория уже известна из RSS-ленты, используем её напрямую
			if c.isValidCategory(category) {
				results = append(results, news.CategorizedArticle{
					Article:  article,
					Category: category,
				})
			} else {
				// Категория из RSS невалидна, отправляем в Gemini
				log.Printf("Warning: invalid RSS category '%s' for article %s, sending to Gemini", category, article.ID)
				articlesForGemini = append(articlesForGemini, article)
			}
		} else {
			// Категории нет, отправляем в Gemini
			articlesForGemini = append(articlesForGemini, article)
		}
	}

	log.Printf("Categorization: %d articles from RSS categories, %d articles need Gemini", len(results), len(articlesForGemini))

	// Обрабатываем статьи без категории через Gemini
	if len(articlesForGemini) > 0 {
		geminiResults, err := c.categorizeWithGemini(ctx, articlesForGemini)
		if err != nil {
			return nil, fmt.Errorf("categorize with Gemini: %w", err)
		}
		results = append(results, geminiResults...)
	}

	log.Printf("Categorization complete: %d total articles categorized", len(results))

	// Логируем распределение по категориям
	categoryCount := make(map[string]int)
	for _, result := range results {
		category := result.Category
		if category == "" {
			category = "Другое / Разное"
		}
		categoryCount[category]++
	}
	log.Println("=== Categorization Distribution ===")
	for category, count := range categoryCount {
		log.Printf("  - %s: %d articles", category, count)
	}
	log.Println("===================================")

	return results, nil
}

// categorizeWithGemini отправляет статьи в Gemini для категоризации.
func (c *Categorizer) categorizeWithGemini(ctx context.Context, articles []news.ArticleRaw) ([]news.CategorizedArticle, error) {
	var results []news.CategorizedArticle

	// Оптимизация: если статей меньше или равно batchSize, обрабатываем все за один запрос
	effectiveBatchSize := c.batchSize
	if len(articles) <= c.batchSize {
		effectiveBatchSize = len(articles)
		log.Printf("Categorizing all %d articles with Gemini in 1 batch (optimization: articles <= batch size)", len(articles))
	} else {
		totalBatches := (len(articles) + c.batchSize - 1) / c.batchSize
		log.Printf("Categorizing %d articles with Gemini in %d batches (batch size: %d)", len(articles), totalBatches, c.batchSize)
	}

	// Задержка 30 секунд между запросами категоризации для соблюдения TPM (не более 2 запросов в минуту)
	const minDelayBetweenRequests = 30 * time.Second
	lastRequestTime := time.Now()
	requestCount := 0

	for i := 0; i < len(articles); i += effectiveBatchSize {
		end := i + effectiveBatchSize
		if end > len(articles) {
			end = len(articles)
		}

		// Соблюдаем задержку между запросами для соблюдения TPM лимита (30 секунд = 2 запроса в минуту)
		elapsed := time.Since(lastRequestTime)
		if elapsed < minDelayBetweenRequests && requestCount > 0 {
			waitTime := minDelayBetweenRequests - elapsed
			log.Printf("Waiting %v before next Gemini categorization request (TPM limit: max 2 requests per minute)...", waitTime)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitTime):
			}
		}

		batch := articles[i:end]
		requestCount++
		totalBatches := (len(articles) + effectiveBatchSize - 1) / effectiveBatchSize
		log.Printf("Processing Gemini categorization batch %d/%d (%d articles)...", requestCount, totalBatches, len(batch))

		batchResults, err := c.categorizeBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("categorize batch [%d-%d]: %w", i, end-1, err)
		}

		results = append(results, batchResults...)
		lastRequestTime = time.Now()
	}

	log.Printf("Gemini categorization complete: %d articles categorized in %d API requests", len(results), requestCount)

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
		// Проверяем, является ли это ошибкой квоты (RPD)
		errStr := err.Error()
		if strings.Contains(strings.ToLower(errStr), "quota") || strings.Contains(strings.ToLower(errStr), "rpd") {
			log.Printf("CRITICAL: Gemini API quota exceeded during categorization. Stopping batch processing.")
			return nil, fmt.Errorf("gemini API quota exceeded (RPD limit): %w", err)
		}
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
