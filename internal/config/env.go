package config

import (
	"fmt"
	"os"
)

// EnvConfig содержит токены и другие переменные окружения.
type EnvConfig struct {
	TelegramBotToken string
	GeminiAPIKey     string
	ForceDispatch    bool
	SkipGemini       bool // Пропустить этапы Gemini (только логи фильтрации)
	SendTestMessage  bool // Отправить только тестовое сообщение без обработки новостей
	BuildMode        bool // Режим формирования дайджеста (сохраняет, не отправляет)
	SendMode         bool // Режим отправки дайджеста (читает сохраненный, отправляет)
}

// LoadEnvConfig читает переменные окружения и возвращает конфигурацию.
// Возвращает ошибку, если обязательные переменные отсутствуют или пустые.
func LoadEnvConfig() (*EnvConfig, error) {
	tgToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if tgToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN environment variable is required")
	}

	skipGemini := os.Getenv("SKIP_GEMINI") == "1"
	sendTestMessage := os.Getenv("SEND_TEST_MESSAGE") == "1"

	// GEMINI_API_KEY обязателен только если не пропускаем Gemini и не отправляем только тестовое сообщение
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if !skipGemini && !sendTestMessage && geminiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is required (or set SKIP_GEMINI=1 or SEND_TEST_MESSAGE=1)")
	}

	forceDispatch := os.Getenv("FORCE_DISPATCH") == "1"
	buildMode := os.Getenv("BUILD_MODE") == "1"
	sendMode := os.Getenv("SEND_MODE") == "1"

	return &EnvConfig{
		TelegramBotToken: tgToken,
		GeminiAPIKey:     geminiKey,
		ForceDispatch:    forceDispatch,
		SkipGemini:       skipGemini,
		SendTestMessage:  sendTestMessage,
		BuildMode:        buildMode,
		SendMode:         sendMode,
	}, nil
}
