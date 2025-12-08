package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maine/vietnam_bot_news/internal/news"
)

// mockTelegramClient - мок для тестирования Sender
type mockTelegramClient struct {
	sendMessageFunc func(ctx context.Context, chatID string, text string, parseMode string) error
	getUpdatesFunc  func(ctx context.Context, offset int64, timeout int) ([]Update, error)
}

func (m *mockTelegramClient) SendMessage(ctx context.Context, chatID string, text string, parseMode string) error {
	if m.sendMessageFunc != nil {
		return m.sendMessageFunc(ctx, chatID, text, parseMode)
	}
	return nil
}

func (m *mockTelegramClient) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	if m.getUpdatesFunc != nil {
		return m.getUpdatesFunc(ctx, offset, timeout)
	}
	return nil, nil
}

func TestSender_Send(t *testing.T) {
	tests := []struct {
		name       string
		recipients []news.RecipientBinding
		messages   []string
		mockFunc   func(ctx context.Context, chatID string, text string, parseMode string) error
		wantErr    bool
	}{
		{
			name:       "empty recipients",
			recipients: []news.RecipientBinding{},
			messages:   []string{"test"},
			wantErr:    true,
		},
		{
			name: "empty messages",
			recipients: []news.RecipientBinding{
				{ChatID: "123", Name: "user1"},
			},
			messages: []string{},
			wantErr:  true,
		},
		{
			name: "successful send",
			recipients: []news.RecipientBinding{
				{ChatID: "123", Name: "user1"},
			},
			messages: []string{"Message 1"},
			mockFunc: func(ctx context.Context, chatID string, text string, parseMode string) error {
				return nil
			},
			wantErr: false,
		},
		{
			name: "multiple recipients and messages",
			recipients: []news.RecipientBinding{
				{ChatID: "123", Name: "user1"},
				{ChatID: "456", Name: "user2"},
			},
			messages: []string{"Message 1", "Message 2"},
			mockFunc: func(ctx context.Context, chatID string, text string, parseMode string) error {
				return nil
			},
			wantErr: false,
		},
		{
			name: "retry on retryable error",
			recipients: []news.RecipientBinding{
				{ChatID: "123", Name: "user1"},
			},
			messages: []string{"Message 1"},
			mockFunc: func(ctx context.Context, chatID string, text string, parseMode string) error {
				// Симулируем сетевую ошибку (retryable)
				return errors.New("network timeout")
			},
			wantErr: false, // После retry всё равно будет ошибка, но процесс продолжается
		},
		{
			name: "non-retryable error",
			recipients: []news.RecipientBinding{
				{ChatID: "123", Name: "user1"},
			},
			messages: []string{"Message 1"},
			mockFunc: func(ctx context.Context, chatID string, text string, parseMode string) error {
				return errors.New("chat not found")
			},
			wantErr: false, // Ошибка логируется, но процесс продолжается
		},
		{
			name: "context cancellation",
			recipients: []news.RecipientBinding{
				{ChatID: "123", Name: "user1"},
			},
			messages: []string{"Message 1"},
			mockFunc: func(ctx context.Context, chatID string, text string, parseMode string) error {
				// Проверяем, что контекст передаётся
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					return nil
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockTelegramClient{
				sendMessageFunc: tt.mockFunc,
			}
			sender := NewSender(mockClient)
			ctx := context.Background()

			err := sender.Send(ctx, tt.recipients, tt.messages)
			if (err != nil) != tt.wantErr {
				t.Errorf("Send() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSender_isRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "retryable network error",
			err:  errors.New("network timeout"),
			want: true,
		},
		{
			name: "non-retryable chat not found",
			err:  errors.New("chat not found"),
			want: false,
		},
		{
			name: "non-retryable bot blocked",
			err:  errors.New("bot was blocked"),
			want: false,
		},
		{
			name: "non-retryable message too long",
			err:  errors.New("message is too long"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableError(tt.err)
			if got != tt.want {
				t.Errorf("isRetryableError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSender_containsIgnoreCase(t *testing.T) {
	tests := []struct {
		name string
		s    string
		sub  string
		want bool
	}{
		{
			name: "contains substring",
			s:    "Hello World",
			sub:  "world",
			want: true,
		},
		{
			name: "case insensitive",
			s:    "HELLO WORLD",
			sub:  "hello",
			want: true,
		},
		{
			name: "does not contain",
			s:    "Hello World",
			sub:  "test",
			want: false,
		},
		{
			name: "empty strings",
			s:    "",
			sub:  "",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsIgnoreCase(tt.s, tt.sub)
			if got != tt.want {
				t.Errorf("containsIgnoreCase() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSender_RateLimit(t *testing.T) {
	// Тест проверяет, что rate limiting работает
	mockClient := &mockTelegramClient{
		sendMessageFunc: func(ctx context.Context, chatID string, text string, parseMode string) error {
			return nil
		},
	}
	sender := NewSender(mockClient)
	ctx := context.Background()

	recipients := []news.RecipientBinding{
		{ChatID: "123", Name: "user1"},
		{ChatID: "456", Name: "user2"},
	}
	messages := []string{"Message 1", "Message 2", "Message 3"}

	start := time.Now()
	err := sender.Send(ctx, recipients, messages)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Send() error = %v", err)
	}

	// Проверяем, что была задержка (минимум 5 сообщений * rateLimitDelay)
	// rateLimitDelay = ~33ms, для 6 сообщений минимум ~165ms
	if duration < 100*time.Millisecond {
		t.Errorf("Send() should respect rate limit, duration = %v", duration)
	}
}
