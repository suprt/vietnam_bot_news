package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/maine/vietnam_bot_news/internal/news"
)

const (
	// telegramRateLimit - лимит Telegram Bot API: 30 сообщений в секунду
	telegramRateLimitPerSecond = 30
	// retryAttempts - количество попыток отправки при ошибке
	retryAttempts = 3
	// retryDelay - задержка между попытками
	retryDelay = 2 * time.Second
	// rateLimitDelay - минимальная задержка между сообщениями для соблюдения rate limit
	rateLimitDelay = time.Second / telegramRateLimitPerSecond // ~33ms между сообщениями
)

// Sender реализует app.Sender для отправки сообщений получателям через Telegram.
type Sender struct {
	client TelegramClient
}

// NewSender создаёт новый экземпляр отправителя.
func NewSender(client TelegramClient) *Sender {
	return &Sender{
		client: client,
	}
}

// Send реализует app.Sender.
// Отправляет каждое сообщение каждому получателю с учётом rate limits и retry-логики.
func (s *Sender) Send(ctx context.Context, recipients []news.RecipientBinding, messages []string) error {
	if len(recipients) == 0 {
		return fmt.Errorf("no recipients provided")
	}
	if len(messages) == 0 {
		return fmt.Errorf("no messages to send")
	}

	totalMessages := len(recipients) * len(messages)
	log.Printf("Sending %d messages to %d recipients (total: %d messages)", len(messages), len(recipients), totalMessages)

	sentCount := 0
	lastSentTime := time.Now()

	for _, recipient := range recipients {
		for _, message := range messages {
			// Контроль rate limit: минимальная задержка между сообщениями
			elapsed := time.Since(lastSentTime)
			if elapsed < rateLimitDelay {
				waitTime := rateLimitDelay - elapsed
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(waitTime):
				}
			}

			// Отправка с retry-логикой
			err := s.sendWithRetry(ctx, recipient.ChatID, message)
			if err != nil {
				// Логируем ошибку, но продолжаем отправку остальным
				log.Printf("Failed to send message to %s (chat_id: %s) after %d attempts: %v",
					recipient.Name, recipient.ChatID, retryAttempts, err)
				// В MVP не прерываем процесс при ошибке одного получателя
				// TODO: можно добавить конфигурацию для критичности ошибок
				continue
			}

			sentCount++
			lastSentTime = time.Now()
		}
	}

	log.Printf("Successfully sent %d/%d messages", sentCount, totalMessages)
	return nil
}

// sendWithRetry отправляет сообщение с повторными попытками при ошибках.
func (s *Sender) sendWithRetry(ctx context.Context, chatID string, message string) error {
	var lastErr error

	for attempt := 0; attempt < retryAttempts; attempt++ {
		if attempt > 0 {
			// Задержка перед повтором (экспоненциальная с максимумом)
			delay := retryDelay * time.Duration(attempt)
			if delay > 10*time.Second {
				delay = 10 * time.Second
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := s.client.SendMessage(ctx, chatID, message, "Markdown")
		if err == nil {
			return nil
		}

		lastErr = err

		// Проверяем, стоит ли повторять попытку
		// Для некоторых ошибок (например, чат не найден, бот заблокирован) повтор не поможет
		if !isRetryableError(err) {
			return err
		}
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

// isRetryableError определяет, можно ли повторить отправку при данной ошибке.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Ошибки, при которых повтор не поможет
	nonRetryableErrors := []string{
		"chat not found",
		"bot was blocked",
		"user is deactivated",
		"chat_id is empty",
		"message is too long",
		"bad request",
	}

	for _, nonRetryable := range nonRetryableErrors {
		// Простая проверка по подстроке (можно улучшить, если Telegram API вернёт коды ошибок)
		// В MVP этого достаточно
		if containsIgnoreCase(errStr, nonRetryable) {
			return false
		}
	}

	// По умолчанию считаем ошибку повторяемой (сетевые ошибки, временные проблемы API)
	return true
}

// containsIgnoreCase проверяет, содержит ли строка подстроку (без учёта регистра).
func containsIgnoreCase(s, substr string) bool {
	s = strings.ToLower(s)
	substr = strings.ToLower(substr)
	return strings.Contains(s, substr)
}
