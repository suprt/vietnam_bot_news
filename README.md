# Vietnam Bot News

Автоматический дайджест вьетнамских новостей на русском языке через Telegram бота.

## Описание

Проект собирает новости из вьетнамских новостных сайтов, категоризирует их с помощью Gemini Flash, создаёт краткие русскоязычные резюме и отправляет подборку дня подписчикам в Telegram. Всё работает автоматически через GitHub Actions.

## Быстрый старт

### Требования

- Go 1.23+
- Gemini API ключ (получить на https://ai.google.dev/)
- Telegram Bot Token (получить у @BotFather)

### Локальный запуск

1. Клонируйте репозиторий
2. Установите зависимости:
   ```bash
   go mod download
   ```
3. Настройте переменные окружения:
   ```bash
   export GEMINI_API_KEY="your-api-key"
   export TELEGRAM_BOT_TOKEN="your-bot-token"
   export FORCE_DISPATCH="0"  # опционально, для тестирования установите "1"
   ```
4. Запустите приложение:
   ```bash
   go run ./cmd/dailyjob
   ```

### Запуск через GitHub Actions

1. Настройте секреты в GitHub:
   - Перейдите в Settings → Secrets and variables → Actions
   - Добавьте `GEMINI_API_KEY`
   - Добавьте `TELEGRAM_BOT_TOKEN`

2. Запустите workflow вручную:
   - Перейдите в Actions → Daily News Digest
   - Нажмите "Run workflow"
   - Для тестирования установите `force_dispatch: 1`

3. Автоматический запуск:
   - Workflow запускается каждый день в 01:00 UTC (08:00 по Вьетнаму)

## Структура проекта

```
vietnam_bot_news/
├── cmd/
│   └── dailyjob/          # Точка входа приложения
├── configs/
│   ├── pipeline.yaml      # Конфигурация пайплайна
│   └── sites.yaml         # Список новостных источников
├── internal/
│   ├── app/               # Главный пайплайн
│   ├── config/            # Загрузка конфигурации
│   ├── filter/            # Фильтрация новостей
│   ├── formatter/         # Форматирование сообщений
│   ├── gemini/            # Интеграция с Gemini API
│   ├── news/              # Типы данных
│   ├── ranking/           # Ранжирование новостей
│   ├── sources/           # Сбор новостей из RSS
│   ├── state/             # Хранение состояния
│   └── telegram/          # Интеграция с Telegram Bot API
├── state/
│   └── state.json         # Состояние (создаётся автоматически)
└── .github/
    └── workflows/
        ├── news_daily.yml     # Основной workflow
        ├── build_digest.yml   # Сборка дайджеста
        └── send_digest.yml    # Отправка дайджеста
```

## Конфигурация

### `configs/pipeline.yaml`

Основные параметры пайплайна:
- Категории новостей
- Лимиты на количество статей
- Параметры фильтрации
- Настройки Gemini API

### `configs/sites.yaml`

Список новостных источников с RSS-лентами.

### Переменные окружения

- `GEMINI_API_KEY` (обязательно) — ключ для Gemini API
- `TELEGRAM_BOT_TOKEN` (обязательно) — токен Telegram бота
- `FORCE_DISPATCH` (опционально) — принудительная рассылка (значение: "1")

## Подписка на дайджест

1. Найдите вашего Telegram бота
2. Отправьте команду `/start` или любое сообщение
3. При следующем запуске пайплайна вы автоматически получите дайджест

## Разработка

### Запуск тестов

```bash
# Все тесты
go test ./...

# Конкретный модуль
go test ./internal/filter -v
```
