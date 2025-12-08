package gemini

import (
	"context"
	"fmt"

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
// SDK автоматически читает GEMINI_API_KEY из переменной окружения.
func NewClient() (*Client, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, nil)
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
