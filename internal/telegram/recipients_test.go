package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/maine/vietnam_bot_news/internal/news"
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
				Recipients: []news.RecipientBinding{
					{ChatID: "123", Name: "user1"},
					{ChatID: "456", Name: "user2"},
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
				manager = NewRecipientManager(mockClient, tt.autoSubscribe)
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

func TestRecipientManager_deriveRecipientName(t *testing.T) {
	tests := []struct {
		name string
		msg  *Message
		want string
	}{
		{
			name: "prefer chat username",
			msg: &Message{
				Chat: Chat{
					Username: "chatuser",
				},
				From: &User{
					Username: "fromuser",
				},
			},
			want: "chatuser",
		},
		{
			name: "use from username if no chat username",
			msg: &Message{
				Chat: Chat{},
				From: &User{
					Username: "fromuser",
				},
			},
			want: "fromuser",
		},
		{
			name: "use chat title",
			msg: &Message{
				Chat: Chat{
					Title: "Group Chat",
				},
			},
			want: "Group Chat",
		},
		{
			name: "use first and last name",
			msg: &Message{
				Chat: Chat{
					FirstName: "John",
					LastName:  "Doe",
				},
			},
			want: "John Doe",
		},
		{
			name: "fallback to chat ID",
			msg: &Message{
				Chat: Chat{
					ID: 12345,
				},
			},
			want: "chat-12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveRecipientName(tt.msg)
			if got != tt.want {
				t.Errorf("deriveRecipientName() = %v, want %v", got, tt.want)
			}
		})
	}
}
