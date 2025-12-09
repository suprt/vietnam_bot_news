package ranking

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/maine/vietnam_bot_news/internal/config"
	"github.com/maine/vietnam_bot_news/internal/gemini"
	"github.com/maine/vietnam_bot_news/internal/news"
)

// Ranker реализует app.Ranker для выбора топ-N новостей в каждой категории через Gemini.
type Ranker struct {
	maxPerCategory int
	geminiClient   gemini.GeminiClient
	cfg            config.Gemini
	batchSize      int
}

// NewRanker создаёт новый экземпляр ранкера.
func NewRanker(cfg config.Pipeline, geminiClient gemini.GeminiClient, geminiCfg config.Gemini) *Ranker {
	batchSize := geminiCfg.BatchSizeRanking
	if batchSize <= 0 {
		batchSize = 10 // дефолтное значение
	}
	maxPerCategory := cfg.MaxArticlesPerCategory
	if maxPerCategory <= 0 {
		maxPerCategory = 5 // дефолтное значение
	}
	return &Ranker{
		maxPerCategory: maxPerCategory,
		geminiClient:   geminiClient,
		cfg:            geminiCfg,
		batchSize:      batchSize,
	}
}

// Rank реализует app.Ranker.
// Группирует новости по категориям, отправляет в Gemini для оценки актуальности,
// затем сортирует и выбирает топ-N в каждой категории.
func (r *Ranker) Rank(ctx context.Context, categorized []news.CategorizedArticle) ([]news.CategorizedArticle, error) {
	if len(categorized) == 0 {
		return nil, nil
	}

	// Группируем по категориям
	byCategory := make(map[string][]news.CategorizedArticle)
	for _, catArticle := range categorized {
		category := catArticle.Category
		if category == "" {
			category = "Другое / Разное"
		}
		byCategory[category] = append(byCategory[category], catArticle)
	}

	var results []news.CategorizedArticle

	// Минимальная задержка между запросами для соблюдения RPM=5 (12 секунд между запросами)
	const minDelayBetweenRequests = 12 * time.Second
	lastRequestTime := time.Now()
	categoryCount := 0
	totalCategories := len(byCategory)

	// Обрабатываем каждую категорию отдельно
	for category, articles := range byCategory {
		categoryCount++

		// Соблюдаем задержку между запросами для соблюдения RPM лимита
		elapsed := time.Since(lastRequestTime)
		if elapsed < minDelayBetweenRequests && categoryCount > 1 {
			waitTime := minDelayBetweenRequests - elapsed
			log.Printf("Waiting %v before ranking category '%s' (RPM limit, %d/%d categories)...", waitTime, category, categoryCount, totalCategories)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitTime):
			}
		}

		// Оцениваем актуальность через Gemini (все статьи категории одним запросом)
		scored, err := r.rankCategory(ctx, category, articles)
		if err != nil {
			// Если ошибка при ранкинге, используем все статьи без сортировки (fallback)
			scored = articles
		}

		// Сортируем по оценке актуальности (убывание)
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].RelevanceScore > scored[j].RelevanceScore
		})

		// Обрезаем до maxPerCategory
		if len(scored) > r.maxPerCategory {
			scored = scored[:r.maxPerCategory]
		}

		results = append(results, scored...)
		lastRequestTime = time.Now()
	}

	// Логируем распределение по категориям после ранкинга
	finalCategoryCount := make(map[string]int)
	for _, result := range results {
		category := result.Category
		if category == "" {
			category = "Другое / Разное"
		}
		finalCategoryCount[category]++
	}
	log.Println("=== Ranking Distribution (after top-N selection) ===")
	for category, count := range finalCategoryCount {
		status := "OK"
		if count < 3 {
			status = "LOW"
		}
		log.Printf("  - %s: %d articles [%s]", category, count, status)
	}
	log.Println("===================================================")

	return results, nil
}

// rankCategory оценивает актуальность новостей в категории через Gemini.
// ВАЖНО: отправляет ВСЕ статьи категории одним запросом для консистентности оценок.
func (r *Ranker) rankCategory(ctx context.Context, category string, articles []news.CategorizedArticle) ([]news.CategorizedArticle, error) {
	if len(articles) == 0 {
		return nil, nil
	}

	// Отправляем все статьи категории одним запросом для консистентного ранкинга
	// Gemini должен видеть все статьи категории одновременно, чтобы правильно их сравнивать
	log.Printf("Ranking all %d articles in category '%s' in 1 request (all articles sent together for consistent ranking)", len(articles), category)

	scored, err := r.rankBatch(ctx, category, articles)
	if err != nil {
		return nil, fmt.Errorf("rank category '%s': %w", category, err)
	}

	log.Printf("Ranking complete for category '%s': %d articles scored", category, len(scored))

	return scored, nil
}

// rankBatch отправляет батч новостей в Gemini для оценки актуальности.
func (r *Ranker) rankBatch(ctx context.Context, category string, articles []news.CategorizedArticle) ([]news.CategorizedArticle, error) {
	// Формируем входные данные для промпта
	inputData := make([]articleRankingInput, 0, len(articles))
	for _, article := range articles {
		inputData = append(inputData, articleRankingInput{
			ID:          article.Article.ID,
			Category:    category,
			Title:       article.Article.Title,
			Content:     article.Article.RawContent,
			PublishedAt: article.Article.PublishedAt.Format("2006-01-02T15:04:05-07:00"),
			Source:      article.Article.Source,
		})
	}

	inputJSON, err := json.Marshal(inputData)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	// Формируем промпт согласно docs/prompting.md
	prompt := r.buildPrompt(string(inputJSON))

	// Вызываем Gemini API
	responseText, err := r.geminiClient.GenerateText(ctx, r.cfg.ModelRanking, prompt)
	if err != nil {
		// Проверяем, является ли это ошибкой квоты (RPD)
		errStr := err.Error()
		if strings.Contains(strings.ToLower(errStr), "quota") || strings.Contains(strings.ToLower(errStr), "rpd") {
			log.Printf("CRITICAL: Gemini API quota exceeded during ranking. Stopping batch processing.")
			return nil, fmt.Errorf("gemini API quota exceeded (RPD limit): %w", err)
		}
		return nil, fmt.Errorf("generate text: %w", err)
	}

	// Парсим JSON ответ
	var scores []relevanceScoreResponse
	if err := json.Unmarshal([]byte(responseText), &scores); err != nil {
		// Пытаемся извлечь JSON из текста, если модель добавила лишнее
		cleaned := extractJSON(responseText)
		if cleaned == "" {
			return nil, fmt.Errorf("unmarshal response: %w (raw: %s)", err, responseText)
		}
		if err := json.Unmarshal([]byte(cleaned), &scores); err != nil {
			return nil, fmt.Errorf("unmarshal cleaned response: %w (cleaned: %s)", err, cleaned)
		}
	}

	// Создаём map для быстрого поиска оценки по ID
	scoresMap := make(map[string]float64, len(scores))
	for _, scoreResp := range scores {
		// Валидируем и нормализуем оценку (0-10)
		score := scoreResp.RelevanceScore
		if score < 0 {
			score = 0
		}
		if score > 10 {
			score = 10
		}
		scoresMap[scoreResp.ID] = score
	}

	// Формируем результат с оценками
	results := make([]news.CategorizedArticle, 0, len(articles))
	for _, article := range articles {
		score, ok := scoresMap[article.Article.ID]
		if !ok {
			// Если Gemini не вернул оценку, используем 5 (средняя оценка) как fallback
			score = 5.0
		}

		article.RelevanceScore = score
		results = append(results, article)
	}

	return results, nil
}

func (r *Ranker) buildPrompt(inputJSON string) string {
	return fmt.Sprintf(`Ты — опытный редактор новостной ленты, который оценивает актуальность и важность новостей.
Тебе будет передан список новостей из одной категории с уникальными идентификаторами id, заголовками, полным текстом, датой публикации и источником на вьетнамском языке (иногда на английском).
Для каждой новости оцени её актуальность и важность по шкале от 0 до 10, где 10 — очень важная и актуальная новость, 0 — неактуальная.
Учитывай: значимость события, актуальность темы, общественный интерес, новизну информации.
Верни результат в виде списка объектов JSON без дополнительных комментариев. Формат:
[{"id": "<id новости>", "relevance_score": <число от 0 до 10>}, ...]

Входные данные:
%s`, inputJSON)
}

// extractJSON извлекает JSON-массив из текста (если модель добавила лишний текст).
func extractJSON(text string) string {
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

type articleRankingInput struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	PublishedAt string `json:"published_at"`
	Source      string `json:"source"`
}

type relevanceScoreResponse struct {
	ID             string  `json:"id"`
	RelevanceScore float64 `json:"relevance_score"`
}
