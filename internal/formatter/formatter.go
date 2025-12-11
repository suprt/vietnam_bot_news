package formatter

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/maine/vietnam_bot_news/internal/config"
	"github.com/maine/vietnam_bot_news/internal/news"
)

const (
	// telegramMaxMessageLength - максимальная длина сообщения в Telegram (4096 символов)
	telegramMaxMessageLength = 4096
	// headerTemplate - шаблон для нумерации сообщений с датой
	headerTemplate = "Подборка дня (%d/%d) — %s\n\n"
	// ellipsis - символы, добавляемые при обрезке сообщения
	ellipsis = "..."
)

// Formatter реализует app.Formatter для форматирования дайджеста в Markdown.
type Formatter struct {
	maxMessages int
}

// NewFormatter создаёт новый экземпляр форматтера.
func NewFormatter(cfg config.Pipeline) *Formatter {
	maxMessages := cfg.MaxTotalMessages
	if maxMessages <= 0 {
		maxMessages = 5 // дефолтное значение
	}
	return &Formatter{
		maxMessages: maxMessages,
	}
}

// BuildMessages реализует app.Formatter.
// Группирует новости по категориям, форматирует в Markdown и разбивает на сообщения.
func (f *Formatter) BuildMessages(entries []news.DigestEntry) ([]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	// Группируем по категориям
	byCategory := make(map[string][]news.DigestEntry)
	for _, entry := range entries {
		category := entry.Category
		if category == "" {
			category = "Другое / Разное"
		}
		byCategory[category] = append(byCategory[category], entry)
	}

	// Форматируем каждую категорию отдельно
	categoryBlocks := f.formatCategoriesAsBlocks(byCategory)

	// Разбиваем на сообщения по блокам категорий (без разрывов)
	// Количество сообщений = количество категорий (без лимита)
	messages := f.splitIntoMessagesByCategories(categoryBlocks)

	return messages, nil
}

// formatCategoriesAsBlocks форматирует каждую категорию отдельно и возвращает массив блоков.
func (f *Formatter) formatCategoriesAsBlocks(byCategory map[string][]news.DigestEntry) []string {
	// Сортируем категории для предсказуемого порядка
	categories := make([]string, 0, len(byCategory))
	for cat := range byCategory {
		categories = append(categories, cat)
	}
	sort.Strings(categories)

	blocks := make([]string, 0, len(categories))
	for _, category := range categories {
		var sb strings.Builder

		// Заголовок категории: *Категория*
		sb.WriteString(fmt.Sprintf("*%s*\n", category))

		entries := byCategory[category]
		for j, entry := range entries {
			// Используем переведенный заголовок, если он есть, иначе оригинальный
			title := entry.TitleRU
			if title == "" {
				title = entry.Title
			}
			// Формат: [Заголовок](URL) — summary
			line := fmt.Sprintf("[%s](%s) — %s", title, entry.URL, entry.SummaryRU)
			sb.WriteString(line)
			if j < len(entries)-1 {
				sb.WriteString("\n")
			}
		}

		blocks = append(blocks, sb.String())
	}

	return blocks
}

// splitIntoMessagesByCategories разбивает блоки категорий на сообщения, не разрывая категории.
// Каждая категория — это отдельный блок, который либо полностью помещается в сообщение, либо разрывается только в крайнем случае.
// Количество сообщений определяется количеством категорий (без лимита).
func (f *Formatter) splitIntoMessagesByCategories(categoryBlocks []string) []string {
	if len(categoryBlocks) == 0 {
		return nil
	}

	var messages []string
	currentMessage := strings.Builder{}

	// Зарезервируем место для заголовка нумерации с датой (примерно 50 символов)
	// Пример: "Подборка дня (1/3) — 15 января 2025\n\n" ≈ 45-50 символов
	const headerReserve = 50
	// Разделитель между категориями
	const categorySeparator = "\n\n"

	for _, block := range categoryBlocks {
		blockWithSeparator := block
		if currentMessage.Len() > 0 {
			blockWithSeparator = categorySeparator + block
		}

		blockLen := len(blockWithSeparator)
		wouldExceed := currentMessage.Len()+blockLen+headerReserve > telegramMaxMessageLength

		// Если текущий блок не помещается в текущее сообщение
		if wouldExceed && currentMessage.Len() > 0 {
			// Сохраняем текущее сообщение
			msg := currentMessage.String()
			messages = append(messages, msg)
			currentMessage.Reset()

			// Начинаем новое сообщение с текущего блока (без разделителя, т.к. это начало)
			blockWithSeparator = block
		}

		// Если даже один блок превышает лимит, нужно его разорвать (крайний случай)
		if len(block)+headerReserve > telegramMaxMessageLength {
			// Разрываем блок построчно только если он один не помещается
			if currentMessage.Len() == 0 {
				// Текущее сообщение пустое, можем разорвать этот блок
				lines := strings.Split(block, "\n")
				for _, line := range lines {
					lineWithNewline := line + "\n"
					lineLen := len(lineWithNewline)

					if currentMessage.Len()+lineLen+headerReserve > telegramMaxMessageLength && currentMessage.Len() > 0 {
						msg := strings.TrimSuffix(currentMessage.String(), "\n")
						messages = append(messages, msg)
						currentMessage.Reset()
					}

					currentMessage.WriteString(lineWithNewline)
				}
				// Переходим к следующему блоку
				continue
			} else {
				// Текущее сообщение не пустое, сохраняем его и начинаем новое
				msg := currentMessage.String()
				messages = append(messages, msg)
				currentMessage.Reset()

				// Теперь разрываем блок построчно
				lines := strings.Split(block, "\n")
				for _, line := range lines {
					lineWithNewline := line + "\n"
					lineLen := len(lineWithNewline)

					if currentMessage.Len()+lineLen+headerReserve > telegramMaxMessageLength && currentMessage.Len() > 0 {
						msg := strings.TrimSuffix(currentMessage.String(), "\n")
						messages = append(messages, msg)
						currentMessage.Reset()
					}

					currentMessage.WriteString(lineWithNewline)
				}
				continue
			}
		}

		// Обычный случай: блок помещается, добавляем его
		currentMessage.WriteString(blockWithSeparator)
	}

	// Добавляем последнее сообщение, если оно не пустое
	if currentMessage.Len() > 0 {
		msg := currentMessage.String()
		messages = append(messages, msg)
	}

	// Добавляем нумерацию ко всем сообщениям, если их больше одного
	if len(messages) > 1 {
		total := len(messages)
		// Форматируем сегодняшнюю дату
		today := time.Now().Format("2 января 2006")
		result := make([]string, 0, total)
		for i, msg := range messages {
			header := fmt.Sprintf(headerTemplate, i+1, total, today)
			// Проверяем, что итоговое сообщение не превышает лимит с учётом заголовка
			fullMessage := header + msg
			if len(fullMessage) > telegramMaxMessageLength {
				// Обрезаем содержимое, учитывая длину заголовка и ellipsis
				maxContentLen := telegramMaxMessageLength - len(header) - len(ellipsis)
				if maxContentLen > 0 {
					if len(msg) > maxContentLen {
						msg = msg[:maxContentLen] + ellipsis
					}
				} else {
					// Если даже заголовок + ellipsis превышают лимит, оставляем только заголовок
					msg = ""
				}
				fullMessage = header + msg
			}
			result = append(result, fullMessage)
		}
		return result
	}

	return messages
}
