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
}

// LoadEnvConfig читает переменные окружения и возвращает конфигурацию.
// Возвращает ошибку, если обязательные переменные отсутствуют или пустые.
func LoadEnvConfig() (*EnvConfig, error) {
	tgToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if tgToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN environment variable is required")
	}

	skipGemini := os.Getenv("SKIP_GEMINI") == "1"

	// GEMINI_API_KEY обязателен только если не пропускаем Gemini
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if !skipGemini && geminiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is required (or set SKIP_GEMINI=1)")
	}

	forceDispatch := os.Getenv("FORCE_DISPATCH") == "1"

	return &EnvConfig{
		TelegramBotToken: tgToken,
		GeminiAPIKey:     geminiKey,
		ForceDispatch:    forceDispatch,
		SkipGemini:       skipGemini,
	}, nil
}
