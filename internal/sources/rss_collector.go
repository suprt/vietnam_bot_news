package sources

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maine/vietnam_bot_news/internal/config"
	"github.com/maine/vietnam_bot_news/internal/news"
)

// RSSCollector загружает новости из RSS-лент.
type RSSCollector struct {
	sites  []config.Site
	client *http.Client
	clock  func() time.Time
}

// NewRSSCollector создаёт новый экземпляр.
func NewRSSCollector(sites []config.Site, client *http.Client, clock func() time.Time) *RSSCollector {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if clock == nil {
		clock = time.Now
	}
	return &RSSCollector{
		sites:  sites,
		client: client,
		clock:  clock,
	}
}

// Collect реализует app.SourceCollector.
func (c *RSSCollector) Collect(ctx context.Context) ([]news.ArticleRaw, error) {
	var results []news.ArticleRaw
	for _, site := range c.sites {
		// Получаем список RSS-лент для обработки
		rssFeeds := c.getRSSFeeds(site)
		if len(rssFeeds) == 0 {
			continue
		}

		// Обрабатываем каждую RSS-ленту
		for _, rssURL := range rssFeeds {
			items, err := c.fetchFeed(ctx, site, rssURL)
			if err != nil {
				// При ошибке одной RSS-ленты продолжаем обработку других
				// Это позволяет частично обработать сайт, даже если одна из RSS-лент недоступна
				log.Printf("Error fetching RSS feed %s for site %s (%s): %v", rssURL, site.ID, site.Name, err)
				continue
			}

			results = append(results, items...)
		}
	}
	return results, nil
}

// getRSSFeeds возвращает список RSS-лент для сайта.
// Поддерживает оба формата: старый (одна RSS) и новый (массив RSS).
func (c *RSSCollector) getRSSFeeds(site config.Site) []string {
	// Приоритет: новый формат (rss_feeds)
	if len(site.RSSFeeds) > 0 {
		return site.RSSFeeds
	}
	// Обратная совместимость: старая одна RSS-лента
	if strings.TrimSpace(site.RSS) != "" {
		return []string{site.RSS}
	}
	return nil
}

func (c *RSSCollector) fetchFeed(ctx context.Context, site config.Site, rssURL string) ([]news.ArticleRaw, error) {
	// Пробуем несколько вариантов User-Agent, если первый не сработает
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
	}

	var lastErr error
	for attempt, userAgent := range userAgents {
		if attempt > 0 {
			// Небольшая задержка перед повтором
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
			}
			log.Printf("Retrying RSS feed %s with different User-Agent (attempt %d/%d)", rssURL, attempt+1, len(userAgents))
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rssURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}

		// Добавляем реалистичные заголовки браузера, чтобы избежать блокировки (403 Forbidden)
		// Некоторые сайты агрессивно блокируют ботов, поэтому имитируем обычный браузер
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, text/html, application/xhtml+xml, */*")
		req.Header.Set("Accept-Language", "vi-VN,vi;q=0.9,en-US;q=0.8,en;q=0.7")
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("Referer", site.URL) // Указываем, что пришли с главной страницы сайта
		req.Header.Set("DNT", "1")
		req.Header.Set("Upgrade-Insecure-Requests", "1")

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("do request: %w", err)
			continue
		}

		// Если получили 403, пробуем другой User-Agent
		if resp.StatusCode == 403 {
			resp.Body.Close()
			lastErr = fmt.Errorf("unexpected status %d (blocked by server)", resp.StatusCode)
			continue
		}

		// Если получили другую ошибку 4xx, не повторяем
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
		}

		// Успешный ответ - обрабатываем
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read body: %w", err)
			continue
		}

		items, err := parseRSSFeed(body)
		if err != nil {
			lastErr = fmt.Errorf("parse RSS: %w", err)
			continue
		}

		articles := make([]news.ArticleRaw, 0, len(items))
		
		// Лимит: обрабатываем только первые 100 статей из RSS (обычно самые свежие)
		// Это защищает от обработки тысяч старых статей, которые могут быть в RSS
		const maxArticlesPerFeed = 100
		itemsToProcess := items
		if len(items) > maxArticlesPerFeed {
			itemsToProcess = items[:maxArticlesPerFeed]
		}
		
		for i, item := range itemsToProcess {
			if item.Link == "" || item.Title == "" {
				continue
			}

			timestamp := parseTime(item.PubDate, c.clock())
			content := strings.TrimSpace(selectContent(item))

			articles = append(articles, news.ArticleRaw{
				ID:          buildArticleID(site.ID, item.Link, timestamp),
				Source:      site.ID,
				Title:       strings.TrimSpace(item.Title),
				URL:         strings.TrimSpace(item.Link),
				PublishedAt: timestamp,
				RawLanguage: detectLanguage(site),
				RawContent:  content,
				Metadata: map[string]string{
					"rss_rank": strconv.Itoa(i),
					"siteName": site.Name,
				},
			})
		}

		return articles, nil
	}

	// Если все попытки не удались, возвращаем последнюю ошибку
	return nil, fmt.Errorf("failed after %d attempts: %w", len(userAgents), lastErr)
}

func detectLanguage(site config.Site) string {
	// Пока все источники вьетнамские, но оставляем точку расширения.
	return "vi"
}

func buildArticleID(siteID, url string, published time.Time) string {
	h := sha1.Sum([]byte(url))
	return fmt.Sprintf("%s-%s-%d", siteID, hex.EncodeToString(h[:8]), published.Unix())
}

func parseTime(value string, fallback time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}

	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		time.RFC3339,
		"Mon, 02 Jan 2006 15:04:05 MST",
		"02 Jan 2006 15:04:05 MST",
	}

	for _, f := range formats {
		if t, err := time.Parse(f, value); err == nil {
			return t
		}
	}

	return fallback
}

func selectContent(item rssItem) string {
	if item.ContentEncoded != "" {
		return item.ContentEncoded
	}
	if item.Description != "" {
		return item.Description
	}
	return item.Title
}

// --- RSS parsing ---

type rssFeed struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title          string `xml:"title"`
	Link           string `xml:"link"`
	Description    string `xml:"description"`
	ContentEncoded string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	PubDate        string `xml:"pubDate"`
}

func parseRSSFeed(data []byte) ([]rssItem, error) {
	// Предварительная обработка: исправляем распространённые проблемы с XML
	// Некоторые RSS-ленты содержат некорректные XML-сущности (например, & без ;)
	data = fixXMLEntities(data)
	
	var feed rssFeed
	// Сначала пытаемся стандартный парсер
	if err := xml.Unmarshal(data, &feed); err != nil {
		// Если не получилось, используем более толерантный декодер
		decoder := xml.NewDecoder(bytes.NewReader(data))
		decoder.Strict = false
		if err := decoder.Decode(&feed); err != nil {
			return nil, fmt.Errorf("parse RSS XML: %w", err)
		}
	}
	return feed.Channel.Items, nil
}

// fixXMLEntities исправляет распространённые проблемы с XML-сущностями в RSS-лентах.
// Некоторые сайты используют & вместо &amp; в тексте.
func fixXMLEntities(data []byte) []byte {
	// Заменяем & на &amp; только если это не валидная XML-сущность
	// Это простая эвристика, но помогает в большинстве случаев
	result := bytes.ReplaceAll(data, []byte("& "), []byte("&amp; "))
	result = bytes.ReplaceAll(result, []byte("&,"), []byte("&amp;,"))
	result = bytes.ReplaceAll(result, []byte("&."), []byte("&amp;."))
	result = bytes.ReplaceAll(result, []byte("&;"), []byte("&amp;;"))
	
	// Исправляем случаи, когда & стоит в конце строки или перед пробелом без сущности
	// Это более сложная логика, но для MVP достаточно простых замен
	return result
}
