package telegram

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maine/vietnam_bot_news/internal/news"
	"github.com/maine/vietnam_bot_news/internal/state"
)

// RecipientManager отвечает за автоматическое добавление пользователей.
type RecipientManager struct {
	client        TelegramClient
	autoSubscribe bool
	idCipher      *state.IDCipher
}

// NewRecipientManager создаёт менеджер.
func NewRecipientManager(client TelegramClient, auto bool, idCipher *state.IDCipher) *RecipientManager {
	return &RecipientManager{
		client:        client,
		autoSubscribe: auto,
		idCipher:      idCipher,
	}
}

// Resolve обновляет состояние и возвращает актуальный список получателей.
func (m *RecipientManager) Resolve(ctx context.Context, state news.State) (news.State, []news.RecipientBinding, error) {
	if m.client == nil {
		return state, nil, fmt.Errorf("telegram client not configured")
	}
	if m.idCipher == nil {
		return state, nil, fmt.Errorf("state encryption is not configured")
	}

	var err error
	state, _, err = m.idCipher.MigrateRecipients(state)
	if err != nil {
		return state, nil, fmt.Errorf("migrate recipients: %w", err)
	}

	recipients := map[string]news.StoredRecipient{}
	for _, r := range state.Recipients {
		if r.ChatID == "" {
			continue
		}
		chatID, legacy, err := m.idCipher.Decrypt(r.ChatID)
		if err != nil {
			return state, nil, fmt.Errorf("decrypt recipient: %w", err)
		}
		if legacy {
			return state, nil, fmt.Errorf("recipient migration did not encrypt chat ID")
		}
		recipients[chatID] = r
	}

	if m.autoSubscribe {
		updates, err := m.client.GetUpdates(ctx, state.Telegram.LastUpdateID+1, 0)
		if err != nil {
			return state, nil, fmt.Errorf("get updates: %w", err)
		}

		var maxUpdateID int64 = state.Telegram.LastUpdateID

		// Собираем последнее сообщение от каждого пользователя
		// Это позволяет обработать только последнюю команду, игнорируя промежуточные
		lastMessages := make(map[string]*Message) // chatID -> последнее сообщение

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
			// Сохраняем последнее сообщение от каждого пользователя
			// (более поздние обновления перезаписывают более ранние)
			lastMessages[chatID] = upd.Message
		}

		// Обрабатываем только последнее сообщение от каждого пользователя
		for chatID, msg := range lastMessages {
			// Обрабатываем команды
			text := strings.TrimSpace(msg.Text)
			textLower := strings.ToLower(text)

			// Команда /stop - отписка
			if textLower == "/stop" || textLower == "/stop@" || strings.HasPrefix(textLower, "/stop ") {
				// Удаляем пользователя из списка получателей
				delete(recipients, chatID)
				log.Printf("User %s unsubscribed via /stop command", chatID)
				continue
			}

			// Команда /start или любое другое сообщение - подписка
			// Добавляем пользователя в список получателей
			recipient, exists := recipients[chatID]
			if !exists {
				recipient.ChatID, err = m.idCipher.Encrypt(chatID)
				if err != nil {
					return state, nil, fmt.Errorf("encrypt recipient: %w", err)
				}
			}
			recipient.UpdatedAt = time.Now()
			recipients[chatID] = recipient
		}

		state.Telegram.LastUpdateID = maxUpdateID
	}

	stored := make([]news.StoredRecipient, 0, len(recipients))
	res := make([]news.RecipientBinding, 0, len(recipients))
	for chatID, r := range recipients {
		stored = append(stored, r)
		res = append(res, news.RecipientBinding{
			ChatID:    chatID,
			UpdatedAt: r.UpdatedAt,
		})
	}

	sort.Slice(stored, func(i, j int) bool {
		return stored[i].ChatID < stored[j].ChatID
	})
	sort.Slice(res, func(i, j int) bool {
		return strings.Compare(res[i].ChatID, res[j].ChatID) < 0
	})

	state.Recipients = stored
	return state, res, nil
}
