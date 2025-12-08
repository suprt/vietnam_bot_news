package telegram

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maine/vietnam_bot_news/internal/news"
)

// RecipientManager отвечает за автоматическое добавление пользователей.
type RecipientManager struct {
	client        TelegramClient
	autoSubscribe bool
}

// NewRecipientManager создаёт менеджер.
func NewRecipientManager(client TelegramClient, auto bool) *RecipientManager {
	return &RecipientManager{
		client:        client,
		autoSubscribe: auto,
	}
}

// Resolve обновляет состояние и возвращает актуальный список получателей.
func (m *RecipientManager) Resolve(ctx context.Context, state news.State) (news.State, []news.RecipientBinding, error) {
	if m.client == nil {
		return state, nil, fmt.Errorf("telegram client not configured")
	}

	recipients := map[string]news.RecipientBinding{}
	for _, r := range state.Recipients {
		if r.ChatID == "" {
			continue
		}
		recipients[r.ChatID] = r
	}

	if m.autoSubscribe {
		updates, err := m.client.GetUpdates(ctx, state.Telegram.LastUpdateID+1, 0)
		if err != nil {
			return state, nil, fmt.Errorf("get updates: %w", err)
		}

		var maxUpdateID int64 = state.Telegram.LastUpdateID
		for _, upd := range updates {
			if upd.UpdateID > maxUpdateID {
				maxUpdateID = upd.UpdateID
			}
			if upd.Message == nil {
				continue
			}
			if upd.Message.Chat.ID == 0 {
				continue
			}

			chatID := strconv.FormatInt(upd.Message.Chat.ID, 10)
			name := deriveRecipientName(upd.Message)

			// Добавляем пользователя в список получателей
			// Приветствие не отправляем, так как бот работает только при запуске workflow
			// и задержка ответа будет выглядеть странно
			recipients[chatID] = news.RecipientBinding{
				Name:      name,
				ChatID:    chatID,
				UpdatedAt: time.Now(),
			}
		}

		state.Telegram.LastUpdateID = maxUpdateID
	}

	res := make([]news.RecipientBinding, 0, len(recipients))
	for _, r := range recipients {
		res = append(res, r)
	}

	sort.Slice(res, func(i, j int) bool {
		return strings.Compare(res[i].Name, res[j].Name) < 0
	})

	state.Recipients = res
	return state, res, nil
}

func deriveRecipientName(msg *Message) string {
	if msg.Chat.Username != "" {
		return msg.Chat.Username
	}
	if msg.From != nil && msg.From.Username != "" {
		return msg.From.Username
	}
	if msg.Chat.Title != "" {
		return msg.Chat.Title
	}
	if msg.Chat.FirstName != "" || msg.Chat.LastName != "" {
		return strings.TrimSpace(msg.Chat.FirstName + " " + msg.Chat.LastName)
	}
	return fmt.Sprintf("chat-%d", msg.Chat.ID)
}
