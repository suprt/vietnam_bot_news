# Настройка внешнего cron через cron-job.org

Этот документ описывает настройку внешнего cron-сервиса (cron-job.org) для запуска GitHub Actions workflow в точное время.

## Зачем нужен внешний cron?

GitHub Actions `schedule` может иметь задержку до 1-1.5 часа. Внешний cron-сервис обеспечивает более точное время запуска.

## Предварительные требования

1. **Personal Access Token в GitHub:**
   - Перейдите: GitHub → Settings → Developer settings → Personal access tokens → Tokens (classic)
   - Создайте новый token с правами `repo` (или `workflow`)
   - Сохраните токен (показывается только один раз!)

## Настройка cron-job.org

### 1. Основной cronjob (ежедневный запуск)

**Параметры:**
- **Title:** `Vietnam News Daily Digest`
- **Address:** 
  ```
  https://api.github.com/repos/{OWNER}/{REPO}/actions/workflows/news_daily.yml/dispatches
  ```
  Замените `{OWNER}` на ваш GitHub username и `{REPO}` на имя репозитория.
  
  **Пример:**
  ```
  https://api.github.com/repos/suprt/vietnam_bot_news/actions/workflows/news_daily.yml/dispatches
  ```

- **Schedule:** `0 1 * * *` (01:00 UTC каждый день, 08:00 по Вьетнаму UTC+7)
- **Request method:** `POST`

**Headers:**
```
Authorization: token YOUR_GITHUB_TOKEN
Accept: application/vnd.github.v3+json
Content-Type: application/json
```

**Важно:** Используйте формат `token YOUR_TOKEN`, а не `Bearer YOUR_TOKEN` и не просто токен!

**Request body (обычный запуск):**
```json
{
  "ref": "main",
  "inputs": {
    "skip_gemini": "0",
    "force_dispatch": "0",
    "send_test_message": "0"
  }
}
```

### 2. Тестовый cronjob (без Gemini API)

Для тестирования без вызовов к Gemini API используйте те же настройки, но измените Request body:

**Request body (тестовый запуск):**
```json
{
  "ref": "main",
  "inputs": {
    "skip_gemini": "1",
    "force_dispatch": "0",
    "send_test_message": "0"
  }
}
```

### 3. Тестовое сообщение (только отправка в Telegram)

Для проверки отправки сообщений в Telegram:

**Request body (только тестовое сообщение):**
```json
{
  "ref": "main",
  "inputs": {
    "skip_gemini": "0",
    "force_dispatch": "1",
    "send_test_message": "1"
  }
}
```

## Важные моменты

1. **Формат workflow_id:** Используйте только имя файла `news_daily.yml`, без пути `.github/workflows/`
2. **Формат токена:** `token YOUR_TOKEN` (не `Bearer` и не просто токен)
3. **Ветка:** Убедитесь, что указана правильная ветка в `ref` (обычно `main`)
4. **Права токена:** Токен должен иметь права `repo` (для приватных репозиториев) или `workflow`

## Проверка настройки

1. Нажмите "Test" в cron-job.org для проверки запроса
2. Проверьте в GitHub Actions, что workflow запустился
3. Убедитесь, что в логах видно `Event: workflow_dispatch`

## Troubleshooting

### Ошибка 404

- Проверьте правильность owner/repo в URL
- Убедитесь, что используете только имя файла: `news_daily.yml`
- Проверьте права токена

### Ошибка 401 (Unauthorized)

- Проверьте формат токена: `token YOUR_TOKEN`
- Убедитесь, что токен не истек
- Проверьте права токена (должен быть `repo` или `workflow`)

### Ошибка 403 (Forbidden)

- Токен не имеет достаточных прав
- Репозиторий приватный, и токен не имеет доступа

## Альтернатива: получение workflow ID

Если использование имени файла не работает, можно получить workflow ID через API:

```bash
curl -H "Authorization: token YOUR_TOKEN" \
  https://api.github.com/repos/{OWNER}/{REPO}/actions/workflows
```

Затем используйте числовой ID вместо имени файла:
```
https://api.github.com/repos/{OWNER}/{REPO}/actions/workflows/{WORKFLOW_ID}/dispatches
```

## Примечание

Встроенный `schedule` в workflow был удален, чтобы избежать двойного запуска. Теперь workflow запускается только через внешний cron или вручную через `workflow_dispatch`.

