package telegram

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/maine/vietnam_bot_news/internal/news"
	"github.com/maine/vietnam_bot_news/internal/state"
)

// mockTelegramClientForRecipients - мок для тестирования RecipientManager
type mockTelegramClientForRecipients struct {
	getUpdatesFunc  func(ctx context.Context, offset int64, timeout int) ([]Update, error)
	sendMessageFunc func(ctx context.Context, chatID string, text string, parseMode string) error
}

func (m *mockTelegramClientForRecipients) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	if m.getUpdatesFunc != nil {
		return m.getUpdatesFunc(ctx, offset, timeout)
	}
	return []Update{}, nil
}

func (m *mockTelegramClientForRecipients) SendMessage(ctx context.Context, chatID string, text string, parseMode string) error {
	if m.sendMessageFunc != nil {
		return m.sendMessageFunc(ctx, chatID, text, parseMode)
	}
	return nil
}

func TestRecipientManager_Resolve(t *testing.T) {
	tests := []struct {
		name          string
		state         news.State
		autoSubscribe bool
		mockFunc      func(ctx context.Context, offset int64, timeout int) ([]Update, error)
		wantErr       bool
		wantCount     int
	}{
		{
			name:          "no client configured",
			state:         news.State{},
			autoSubscribe: false,
			mockFunc:      nil,
			wantErr:       true,
		},
		{
			name: "existing recipients without auto-subscribe",
			state: news.State{
				Recipients: []news.StoredRecipient{
					{ChatID: "123"},
					{ChatID: "456"},
				},
			},
			autoSubscribe: false,
			mockFunc:      nil,
			wantErr:       false,
			wantCount:     2,
		},
		{
			name: "auto-subscribe with new user",
			state: news.State{
				Telegram: news.TelegramState{LastUpdateID: 0},
			},
			autoSubscribe: true,
			mockFunc: func(ctx context.Context, offset int64, timeout int) ([]Update, error) {
				return []Update{
					{
						UpdateID: 1,
						Message: &Message{
							Chat: Chat{
								ID:       123,
								Type:     "private",
								Username: "testuser",
							},
							Text: "/start",
						},
					},
				}, nil
			},
			wantErr:   false,
			wantCount: 1,
		},
		{
			name: "auto-subscribe with multiple users",
			state: news.State{
				Telegram: news.TelegramState{LastUpdateID: 0},
			},
			autoSubscribe: true,
			mockFunc: func(ctx context.Context, offset int64, timeout int) ([]Update, error) {
				return []Update{
					{
						UpdateID: 1,
						Message: &Message{
							Chat: Chat{
								ID:       123,
								Type:     "private",
								Username: "user1",
							},
						},
					},
					{
						UpdateID: 2,
						Message: &Message{
							Chat: Chat{
								ID:       456,
								Type:     "private",
								Username: "user2",
							},
						},
					},
				}, nil
			},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name: "auto-subscribe error handling",
			state: news.State{
				Telegram: news.TelegramState{LastUpdateID: 0},
			},
			autoSubscribe: true,
			mockFunc: func(ctx context.Context, offset int64, timeout int) ([]Update, error) {
				return nil, errors.New("telegram api error")
			},
			wantErr: true,
		},
		{
			name: "filter invalid updates",
			state: news.State{
				Telegram: news.TelegramState{LastUpdateID: 0},
			},
			autoSubscribe: true,
			mockFunc: func(ctx context.Context, offset int64, timeout int) ([]Update, error) {
				return []Update{
					{
						UpdateID: 1,
						Message:  nil, // Нет сообщения
					},
					{
						UpdateID: 2,
						Message: &Message{
							Chat: Chat{ID: 0}, // Некорректный chat ID
						},
					},
					{
						UpdateID: 3,
						Message: &Message{
							Chat: Chat{
								ID:       123,
								Type:     "private",
								Username: "validuser",
							},
						},
					},
				}, nil
			},
			wantErr:   false,
			wantCount: 1, // Только валидный update
		},
		{
			name: "update LastUpdateID",
			state: news.State{
				Telegram: news.TelegramState{LastUpdateID: 5},
			},
			autoSubscribe: true,
			mockFunc: func(ctx context.Context, offset int64, timeout int) ([]Update, error) {
				return []Update{
					{
						UpdateID: 10,
						Message: &Message{
							Chat: Chat{
								ID:       123,
								Type:     "private",
								Username: "user1",
							},
						},
					},
				}, nil
			},
			wantErr:   false,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var manager *RecipientManager
			if tt.name == "no client configured" {
				manager = &RecipientManager{
					client:        nil,
					autoSubscribe: tt.autoSubscribe,
				}
			} else {
				mockClient := &mockTelegramClientForRecipients{
					getUpdatesFunc: tt.mockFunc,
				}
				manager = NewRecipientManager(mockClient, tt.autoSubscribe, newTestIDCipher(t))
			}

			ctx := context.Background()
			state, recipients, err := manager.Resolve(ctx, tt.state)

			if (err != nil) != tt.wantErr {
				t.Errorf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(recipients) != tt.wantCount {
					t.Errorf("Resolve() recipients count = %v, want %v", len(recipients), tt.wantCount)
				}
				// Проверяем, что state обновлён
				if tt.autoSubscribe && tt.mockFunc != nil {
					// LastUpdateID должен быть обновлён
					if state.Telegram.LastUpdateID < tt.state.Telegram.LastUpdateID {
						t.Errorf("Resolve() LastUpdateID should be updated")
					}
				}
			}
		})
	}
}

func TestRecipientManager_MigratesLegacyRecipient(t *testing.T) {
	manager := NewRecipientManager(&mockTelegramClientForRecipients{}, false, newTestIDCipher(t))
	state := news.State{
		Recipients: []news.StoredRecipient{{ChatID: "12345"}},
	}

	migrated, recipients, err := manager.Resolve(context.Background(), state)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(migrated.Recipients) != 1 || !strings.HasPrefix(migrated.Recipients[0].ChatID, "v1:") {
		t.Fatalf("Resolve() should migrate legacy recipient, got %#v", migrated.Recipients)
	}
	if len(recipients) != 1 || recipients[0].ChatID != "12345" {
		t.Fatalf("Resolve() should return plaintext runtime recipient, got %#v", recipients)
	}
}

func newTestIDCipher(t *testing.T) *state.IDCipher {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cipher, err := state.NewIDCipher(key)
	if err != nil {
		t.Fatalf("NewIDCipher() error = %v", err)
	}
	return cipher
}
