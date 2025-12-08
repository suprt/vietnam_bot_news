package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type (
	// Root объединяет все конфигурационные блоки.
	Root struct {
		Pipeline Pipeline `yaml:"pipeline"`
		Gemini   Gemini   `yaml:"gemini"`
	}

	// Pipeline описывает параметры главного пайплайна (см. docs/architecture.md).
	Pipeline struct {
		MaxArticlesPerCategory  int      `yaml:"max_articles_per_category"`
		Categories              []string `yaml:"categories"`
		RecencyMaxHours         int      `yaml:"recency_max_hours"`
		MinContentLength        int      `yaml:"min_content_length"`
		MaxTotalMessages        int      `yaml:"max_total_messages"`
		MaxArticlesBeforeGemini int      `yaml:"max_articles_before_gemini"` // Лимит статей перед отправкой в Gemini (для оптимизации RPD)
		AutoSubscribe           bool     `yaml:"auto_subscribe"`
		ForceDispatchEnv        string   `yaml:"force_dispatch_env"`
	}

	// Gemini содержит настройки моделей и размеров батчей.
	Gemini struct {
		ModelCategorization   string `yaml:"model_categorization"`
		ModelSummary          string `yaml:"model_summary"`
		ModelRanking          string `yaml:"model_ranking"`
		BatchSizeCategorization int   `yaml:"batch_size_categorization"`
		BatchSizeSummary      int    `yaml:"batch_size_summary"`
		BatchSizeRanking      int    `yaml:"batch_size_ranking"`
	}

	// SitesRoot описывает список источников для парсинга.
	SitesRoot struct {
		Sites []Site `yaml:"sites"`
	}

	// Site соответствует одной записи из docs/sites.md.
	Site struct {
		ID        string   `yaml:"id"`
		Name      string   `yaml:"name"`
		URL       string   `yaml:"url"`
		RSS       string   `yaml:"rss,omitempty"`       // Обратная совместимость: одна RSS-лента
		RSSFeeds  []string `yaml:"rss_feeds,omitempty"` // Новый формат: массив RSS-лент (по категориям)
		Priority  int      `yaml:"priority"`
	}

)

// LoadRoot читает основной файл конфигурации.
func LoadRoot(path string) (Root, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Root{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Root
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Root{}, fmt.Errorf("unmarshal config: %w", err)
	}
	return cfg, nil
}

// LoadSites читаёт конфиг со списком источников.
func LoadSites(path string) (SitesRoot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SitesRoot{}, fmt.Errorf("read sites config: %w", err)
	}

	var cfg SitesRoot
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return SitesRoot{}, fmt.Errorf("unmarshal sites config: %w", err)
	}
	return cfg, nil
}


