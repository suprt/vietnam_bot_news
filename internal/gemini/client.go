package gemini

import (
	"context"
	"fmt"
	"os"

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
func (c *Client) GenerateText(ctx context.Context, model string, prompt string) (string, error) {
	result, err := c.client.Models.GenerateContent(
		ctx,
		model,
		genai.Text(prompt),
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("generate content: %w", err)
	}

	return result.Text()
}
