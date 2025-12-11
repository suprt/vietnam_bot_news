package gemini

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
)

// GeminiClient определяет интерфейс для работы с Gemini API.
// Это позволяет легко создавать моки для тестирования.
type GeminiClient interface {
	GenerateText(ctx context.Context, model string, prompt string) (string, error)
}

// Client инкапсулирует работу с Gemini API через официальный SDK.
type Client struct {
	client *genai.Client
}

// Убеждаемся, что Client реализует интерфейс GeminiClient.
var _ GeminiClient = (*Client)(nil)

// NewClient создаёт новый клиент для работы с Gemini API.
// Читает GEMINI_API_KEY из переменной окружения и явно передаёт его в SDK.
func NewClient() (*Client, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is required")
	}

	ctx := context.Background()

	// Создаём конфигурацию с явно указанным API ключом
	config := &genai.ClientConfig{
		APIKey: apiKey,
	}

	client, err := genai.NewClient(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create genai client: %w", err)
	}

	return &Client{
		client: client,
	}, nil
}

// GenerateText отправляет запрос к Gemini API и возвращает текстовый ответ.
// model - имя модели (например, "gemini-2.5-flash")
// prompt - текстовый промпт для модели
// Включает обработку ошибок лимитов и retry-логику для временных ошибок (503, 500, 502, 504).
func (c *Client) GenerateText(ctx context.Context, model string, prompt string) (string, error) {
	const maxRetries = 5 // Увеличено для временных ошибок
	const baseDelay = 12 * time.Second // Минимум 12 секунд между запросами для соблюдения RPM=5
	const serviceUnavailableDelay = 5 * time.Minute // 5 минут для ошибки 503 (модель перегружена)

	var lastErr error
	var isServiceUnavailable bool
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			var delay time.Duration
			if isServiceUnavailable {
				// Для 503 ошибки используем фиксированную паузу 5 минут
				delay = serviceUnavailableDelay
				log.Printf("Service unavailable (503) - waiting 5 minutes before retry (attempt %d/%d)...", attempt+1, maxRetries)
			} else {
				// Для других ошибок - экспоненциальная задержка с минимумом для соблюдения RPM
				delay = baseDelay * time.Duration(attempt)
				if delay > 60*time.Second {
					delay = 60 * time.Second
				}
				log.Printf("Retrying Gemini API request (attempt %d/%d) after %v...", attempt+1, maxRetries, delay)
			}

			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}

		result, err := c.client.Models.GenerateContent(
			ctx,
			model,
			genai.Text(prompt),
			nil,
		)
		if err == nil {
			text, textErr := result.Text()
			if textErr != nil {
				return "", fmt.Errorf("get text from result: %w", textErr)
			}
			return text, nil
		}

		lastErr = err
		errStr := err.Error()

		// Проверяем тип ошибки
		if isRateLimitError(errStr) {
			log.Printf("Rate limit error from Gemini API: %v", err)
			isServiceUnavailable = false
			// Продолжаем retry
			continue
		}

		if isServiceUnavailableError(errStr) {
			log.Printf("Service unavailable (503) from Gemini API - model overloaded: %v", err)
			isServiceUnavailable = true
			// Продолжаем retry с длинной паузой
			continue
		}

		if isTemporaryError(errStr) {
			log.Printf("Temporary error from Gemini API (500/502/504): %v", err)
			isServiceUnavailable = false
			// Продолжаем retry для временных ошибок
			continue
		}

		if isQuotaExceededError(errStr) {
			// Quota exceeded - не повторяем, это критическая ошибка
			return "", fmt.Errorf("gemini API quota exceeded (RPD limit reached): %w", err)
		}

		// Для других ошибок не повторяем
		return "", fmt.Errorf("generate content: %w", err)
	}

	return "", fmt.Errorf("max retries exceeded: %w", lastErr)
}

// isRateLimitError проверяет, является ли ошибка связанной с rate limit (RPM).
func isRateLimitError(errStr string) bool {
	errLower := strings.ToLower(errStr)
	return strings.Contains(errLower, "rate limit") ||
		strings.Contains(errLower, "429") ||
		strings.Contains(errLower, "too many requests") ||
		strings.Contains(errLower, "resource exhausted")
}

// isServiceUnavailableError проверяет, является ли ошибка 503 (Service Unavailable).
// Это означает, что модель перегружена и требуется длительная пауза (5 минут).
func isServiceUnavailableError(errStr string) bool {
	errLower := strings.ToLower(errStr)
	return strings.Contains(errLower, "503") ||
		strings.Contains(errLower, "service unavailable") ||
		strings.Contains(errLower, "overloaded") ||
		strings.Contains(errLower, "model overloaded")
}

// isTemporaryError проверяет, является ли ошибка временной (500, 502, 504).
// Эти ошибки означают, что сервис временно недоступен, но может восстановиться быстрее, чем при 503.
func isTemporaryError(errStr string) bool {
	errLower := strings.ToLower(errStr)
	return strings.Contains(errLower, "500") ||
		strings.Contains(errLower, "502") ||
		strings.Contains(errLower, "504") ||
		strings.Contains(errLower, "internal server error") ||
		strings.Contains(errLower, "bad gateway") ||
		strings.Contains(errLower, "gateway timeout")
}

// isQuotaExceededError проверяет, является ли ошибка связанной с превышением квоты (RPD).
func isQuotaExceededError(errStr string) bool {
	errLower := strings.ToLower(errStr)
	return strings.Contains(errLower, "quota") ||
		strings.Contains(errLower, "quota exceeded") ||
		strings.Contains(errLower, "daily limit") ||
		strings.Contains(errLower, "403")
}
