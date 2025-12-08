package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/maine/vietnam_bot_news/internal/app"
	"github.com/maine/vietnam_bot_news/internal/config"
	"github.com/maine/vietnam_bot_news/internal/filter"
	"github.com/maine/vietnam_bot_news/internal/formatter"
	"github.com/maine/vietnam_bot_news/internal/gemini"
	"github.com/maine/vietnam_bot_news/internal/ranking"
	"github.com/maine/vietnam_bot_news/internal/sources"
	"github.com/maine/vietnam_bot_news/internal/state"
	"github.com/maine/vietnam_bot_news/internal/telegram"
)

func main() {
	ctx := context.Background()

	// Загружаем конфигурацию из YAML
	rootCfg, err := config.LoadRoot("configs/pipeline.yaml")
	if err != nil {
		log.Fatalf("load pipeline config: %v", err)
	}

	sitesCfg, err := config.LoadSites("configs/sites.yaml")
	if err != nil {
		log.Fatalf("load sites config: %v", err)
	}

	// Загружаем переменные окружения (токены)
	envCfg, err := config.LoadEnvConfig()
	if err != nil {
		log.Fatalf("load env config: %v", err)
	}

	// Инициализируем модули
	httpClient := &http.Client{Timeout: 15 * time.Second}
	collector := sources.NewRSSCollector(sitesCfg.Sites, httpClient, time.Now)
	f := filter.New(rootCfg.Pipeline)
	stateStore := state.NewFileStore("state/state.json")
	tgClient := telegram.NewClient(envCfg.TelegramBotToken)

	// Инициализируем Gemini клиент только если не пропускаем Gemini
	var geminiClient *gemini.Client
	var categorizer app.Categorizer
	var ranker app.Ranker
	var summarizer app.Summarizer
	var msgFormatter app.Formatter
	var sender app.Sender

	if !envCfg.SkipGemini {
		// Клиент явно читает GEMINI_API_KEY из переменной окружения
		var err error
		geminiClient, err = gemini.NewClient()
		if err != nil {
			log.Fatalf("failed to create Gemini client: %v", err)
		}

		// Инициализируем все модули пайплайна
		categorizer = gemini.NewCategorizer(geminiClient, rootCfg.Gemini, rootCfg.Pipeline)
		ranker = ranking.NewRanker(rootCfg.Pipeline, geminiClient, rootCfg.Gemini)
		summarizer = gemini.NewSummarizer(geminiClient, rootCfg.Gemini)
		msgFormatter = formatter.NewFormatter(rootCfg.Pipeline)
		sender = telegram.NewSender(tgClient)
	} else {
		// Если пропускаем Gemini, все равно инициализируем sender для тестового сообщения
		sender = telegram.NewSender(tgClient)
	}

	var recipientResolver app.RecipientResolver
	if rootCfg.Pipeline.AutoSubscribe {
		recipientResolver = telegram.NewRecipientManager(tgClient, true)
	}

	// Создаём пайплайн
	p := app.NewPipeline(app.PipelineDeps{
		Collector:     collector,
		Filter:        f,
		Categorizer:   categorizer,
		Ranker:        ranker,
		Summarizer:    summarizer,
		Formatter:     msgFormatter,
		Sender:        sender,
		Recipients:    recipientResolver,
		StateStore:    stateStore,
		Clock:         nil, // используем time.Now по умолчанию
		ForceDispatch: envCfg.ForceDispatch,
		SkipGemini:    envCfg.SkipGemini,
		Config:        rootCfg.Pipeline,
	})

	if err := p.Run(ctx); err != nil {
		log.Fatalf("pipeline failed: %v", err)
	}

	log.Println("pipeline completed successfully")
}
