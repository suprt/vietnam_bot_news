package sources

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rssURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	// Добавляем User-Agent, чтобы избежать блокировки (403 Forbidden)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; RSSBot/1.0; +https://github.com/maine/vietnam_bot_news)")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	items, err := parseRSSFeed(body)
	if err != nil {
		return nil, fmt.Errorf("parse RSS: %w", err)
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
	// Исправляем некорректные XML-сущности (например, & без ;)
	data = fixXMLEntities(data)

	var feed rssFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, err
	}
	return feed.Channel.Items, nil
}

// fixXMLEntities исправляет распространённые проблемы с XML-сущностями в RSS-лентах.
// Некоторые сайты используют & вместо &amp; в тексте.
// Заменяем & на &amp; только если это не валидная XML-сущность.
func fixXMLEntities(data []byte) []byte {
	// Регулярное выражение для поиска &, которые не являются частью валидной XML-сущности
	// Валидные сущности: &amp; &lt; &gt; &quot; &apos; &#123; &#x1F; &name;
	// Ищем &, за которым не следует валидная сущность
	invalidEntityRegex := regexp.MustCompile(`&(?!(?:amp|lt|gt|quot|apos|#\d+|#x[0-9a-fA-F]+|[a-zA-Z][a-zA-Z0-9]*);)`)

	// Заменяем некорректные & на &amp;
	result := invalidEntityRegex.ReplaceAll(data, []byte("&amp;"))

	return result
}
