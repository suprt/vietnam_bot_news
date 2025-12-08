package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// RSSFeedInfo содержит информацию о найденном RSS-фиде
type RSSFeedInfo struct {
	URL    string `yaml:"-"`
	Type   string `yaml:"-"` // "main" или "category"
	Source string `yaml:"-"`
}

// SiteConfig представляет конфигурацию сайта из YAML
type SiteConfig struct {
	ID       string   `yaml:"id"`
	Name     string   `yaml:"name"`
	URL      string   `yaml:"url"`
	RSS      string   `yaml:"rss,omitempty"`
	RSSFeeds []string `yaml:"rss_feeds,omitempty"`
	Priority int      `yaml:"priority"`
}

// SitesConfig представляет корневой конфиг
type SitesConfig struct {
	Sites []SiteConfig `yaml:"sites"`
}

// OutputSiteConfig для сохранения результатов
type OutputSiteConfig struct {
	ID       string   `yaml:"id"`
	Name     string   `yaml:"name"`
	URL      string   `yaml:"url"`
	RSSFeeds []string `yaml:"rss_feeds"`
	Priority int      `yaml:"priority"`
}

// OutputConfig для сохранения результатов
type OutputConfig struct {
	Sites []OutputSiteConfig `yaml:"sites"`
}

var (
	visitedRSS  = make(map[string]bool)
	allFoundRSS []RSSFeedInfo
	httpClient  = &http.Client{
		Timeout: 10 * time.Second, // Таймаут 10 секунд для надежности
	}
)

func normalizeURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	rawURL = strings.TrimSuffix(rawURL, "/")
	return rawURL
}

func isValidRSSURL(rawURL string) bool {
	urlLower := strings.ToLower(rawURL)
	patterns := []string{"/rss", "/feed", ".rss", ".xml", "/atom"}
	for _, pattern := range patterns {
		if strings.Contains(urlLower, pattern) {
			return true
		}
	}
	return strings.HasSuffix(urlLower, "/rss") || strings.HasSuffix(urlLower, "/feed")
}

func extractRSSFromHTML(htmlContent, baseURL string) map[string]bool {
	rssLinks := make(map[string]bool)

	// Паттерны для поиска RSS-ссылок в HTML
	patterns := []*regexp.Regexp{
		// Стандартные <link> теги с RSS
		regexp.MustCompile(`(?i)<link[^>]*rel=["']alternate["'][^>]*type=["']application/rss\+xml["'][^>]*href=["']([^"']+)["']`),
		regexp.MustCompile(`(?i)<link[^>]*type=["']application/rss\+xml["'][^>]*rel=["']alternate["'][^>]*href=["']([^"']+)["']`),
		// Все href атрибуты, содержащие rss/feed (включая относительные пути)
		regexp.MustCompile(`(?i)href=["']([^"']*(?:rss|feed|\.rss|\.xml)[^"']*)["']`),
		// Ссылки в <a> тегах, где href содержит rss/feed (включая относительные пути типа /rss/...)
		regexp.MustCompile(`(?i)<a[^>]*href=["']([^"']*(?:rss|feed|/rss/|\.rss)[^"']*)["'][^>]*>`),
		// Ссылки в <a> тегах, где текст или title содержит RSS/Feed
		regexp.MustCompile(`(?i)<a[^>]*(?:title=["'][^"']*rss[^"']*["']|>.*?rss.*?</a)[^>]*href=["']([^"']+)["']`),
		regexp.MustCompile(`(?i)<a[^>]*href=["']([^"']+)["'][^>]*(?:title=["'][^"']*rss[^"']*["']|>.*?rss.*?</a)`),
		// Ссылки, которые могут быть в списках категорий (например, <li><a href="...rss...">)
		regexp.MustCompile(`(?i)<li[^>]*>.*?<a[^>]*href=["']([^"']*(?:rss|feed)[^"']*)["']`),
		// Прямые ссылки в тексте (для случаев, когда RSS-ссылки просто перечислены)
		regexp.MustCompile(`(?i)(https?://[^\s<>"']+/(?:rss|feed|category/[^/]+/rss|rss/[^/]+)[^\s<>"']*)`),
	}

	for _, pattern := range patterns {
		matches := pattern.FindAllStringSubmatch(htmlContent, -1)
		for _, match := range matches {
			// Проверяем все группы захвата
			for i := 1; i < len(match); i++ {
				if match[i] != "" {
					href := strings.TrimSpace(match[i])
					// Пропускаем пустые и якорные ссылки
					if href == "" || strings.HasPrefix(href, "#") {
						continue
					}

					fullURL, err := resolveURL(baseURL, href)
					if err != nil {
						continue
					}

					// Проверяем, похож ли URL на RSS
					if isValidRSSURL(fullURL) {
						normalized := normalizeURL(fullURL)
						// Исключаем саму базовую страницу, если она не RSS
						if normalized != normalizeURL(baseURL) || isValidRSSURL(baseURL) {
							rssLinks[normalized] = true
						}
					}
				}
			}
		}
	}

	// Дополнительный поиск: ищем все абсолютные URL на странице, содержащие rss/feed
	urlPattern := regexp.MustCompile(`(?i)(https?://[^\s<>"']+/(?:rss|feed|category/[^/]+/rss|rss/[^/]+|feed/[^/]+)[^\s<>"']*)`)
	urlMatches := urlPattern.FindAllString(htmlContent, -1)
	for _, match := range urlMatches {
		match = strings.TrimSpace(match)
		// Убираем возможные завершающие символы
		match = strings.TrimRight(match, ".,;:)!?\"'")
		if isValidRSSURL(match) {
			normalized := normalizeURL(match)
			if normalized != normalizeURL(baseURL) {
				rssLinks[normalized] = true
			}
		}
	}

	return rssLinks
}

func extractRSSFromXML(xmlContent, baseURL string) map[string]bool {
	rssLinks := make(map[string]bool)

	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Link        string `xml:"link"`
			Description string `xml:"description"`
		} `xml:"channel"`
	}

	// Пробуем распарсить как RSS
	if err := xml.Unmarshal([]byte(xmlContent), &feed); err == nil {
		if feed.Channel.Link != "" {
			fullURL, err := resolveURL(baseURL, feed.Channel.Link)
			if err == nil && isValidRSSURL(fullURL) {
				rssLinks[normalizeURL(fullURL)] = true
			}
		}
	}

	// Ищем все элементы <link> в XML
	decoder := xml.NewDecoder(strings.NewReader(xmlContent))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if se, ok := token.(xml.StartElement); ok {
			if se.Name.Local == "link" {
				var linkText string
				if err := decoder.DecodeElement(&linkText, &se); err == nil {
					linkText = strings.TrimSpace(linkText)
					if linkText != "" {
						fullURL, err := resolveURL(baseURL, linkText)
						if err == nil && isValidRSSURL(fullURL) {
							rssLinks[normalizeURL(fullURL)] = true
						}
					}
				}
			}
		}
	}

	// Ищем URL в описании
	urlPattern := regexp.MustCompile(`https?://[^\s<>"']+(?:rss|feed|\.rss|\.xml)`)
	matches := urlPattern.FindAllString(xmlContent, -1)
	for _, match := range matches {
		if isValidRSSURL(match) {
			rssLinks[normalizeURL(match)] = true
		}
	}

	return rssLinks
}

func resolveURL(baseURL, relativeURL string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	rel, err := url.Parse(relativeURL)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(rel).String(), nil
}

func fetchRSSPage(rssURL string) (string, string, error) {
	req, err := http.NewRequest("GET", rssURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	// Настраиваем клиент для отслеживания редиректов
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Разрешаем до 5 редиректов
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		// Для 403 (Forbidden) просто возвращаем ошибку, но не критично
		if resp.StatusCode == 403 {
			return "", "", fmt.Errorf("forbidden (403) - возможно, сайт блокирует запросы")
		}
		return "", "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	contentType := resp.Header.Get("Content-Type")
	finalURL := resp.Request.URL.String()
	if finalURL != rssURL {
		fmt.Printf("    🔄 Редирект: %s -> %s\n", rssURL, finalURL)
	}

	return string(content), strings.ToLower(contentType), nil
}

func findCategoryRSSFromPage(rssURL string, maxDepth int) map[string]bool {
	if maxDepth <= 0 {
		return make(map[string]bool)
	}

	// Проверяем, не посещали ли уже этот URL
	alreadyVisited := visitedRSS[rssURL]
	if !alreadyVisited {
		visitedRSS[rssURL] = true
	}

	foundRSS := make(map[string]bool)

	fmt.Printf("  📡 Проверяю: %s\n", rssURL)

	content, contentType, err := fetchRSSPage(rssURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠️  Ошибка при загрузке %s: %v\n", rssURL, err)
		return foundRSS
	}

	// Определяем тип контента более точно
	contentPreview := content[:min(1000, len(content))]
	isXML := strings.Contains(contentType, "xml") || strings.HasPrefix(content, "<?xml") || strings.Contains(contentPreview, "<rss")
	isHTML := strings.Contains(contentType, "html") || strings.Contains(strings.ToLower(contentPreview), "<html") || strings.Contains(strings.ToLower(contentPreview), "<!doctype")

	// Если это XML/RSS, парсим его
	if isXML && !isHTML {
		xmlRSS := extractRSSFromXML(content, rssURL)
		for k := range xmlRSS {
			foundRSS[k] = true
		}
	}

	// ВСЕГДА проверяем как HTML (даже если это XML), так как некоторые сайты возвращают HTML на RSS-URL
	// Особенно важно для случаев, когда URL ведет на HTML-страницу со списком RSS
	htmlRSS := extractRSSFromHTML(content, rssURL)
	if len(htmlRSS) > 0 {
		fmt.Printf("    ✅ Найдено %d RSS-ссылок\n", len(htmlRSS))
		// Добавляем все найденные RSS-ссылки без ограничений
		for k := range htmlRSS {
			foundRSS[k] = true
		}
	}

	// Если нашли новые RSS, рекурсивно проверяем их (только для XML, не для HTML)
	// Для HTML-страниц не делаем рекурсию, чтобы избежать зацикливания
	newRSS := make(map[string]bool)
	for k := range foundRSS {
		if !visitedRSS[k] {
			newRSS[k] = true
		}
	}

	// ОТКЛЮЧАЕМ рекурсию полностью для HTML-страниц, чтобы избежать зацикливания
	// Для XML/RSS тоже ограничиваем рекурсию строго
	if len(newRSS) > 0 && maxDepth > 1 && isXML && !isHTML {
		count := 0
		maxRecursive := 3 // Максимум 3 рекурсивных проверки для скорости
		for newURL := range newRSS {
			if count >= maxRecursive {
				break
			}
			recursiveRSS := findCategoryRSSFromPage(newURL, maxDepth-1)
			for k := range recursiveRSS {
				foundRSS[k] = true
			}
			count++
		}
	}

	return foundRSS
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	sitesFile := "configs/sites.yaml"

	fmt.Println("🚀 Начинаю сбор RSS-ссылок из sites.yaml")
	fmt.Println()

	// Читаем sites.yaml
	data, err := os.ReadFile(sitesFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Файл %s не найден: %v\n", sitesFile, err)
		os.Exit(1)
	}

	var config SitesConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Ошибка при чтении %s: %v\n", sitesFile, err)
		os.Exit(1)
	}

	fmt.Printf("📋 Найдено %d сайтов\n\n", len(config.Sites))

	// Собираем все RSS-ссылки
	for _, site := range config.Sites {
		fmt.Printf("\n🌐 %s (%s)\n", site.Name, site.ID)
		fmt.Printf("   URL: %s\n", site.URL)

		// Получаем список RSS-фидов
		rssFeeds := site.RSSFeeds
		if len(rssFeeds) == 0 && site.RSS != "" {
			rssFeeds = []string{site.RSS}
		}

		if len(rssFeeds) == 0 {
			fmt.Println("   ⚠️  RSS-фиды не найдены")
			continue
		}

		var siteRSSList []RSSFeedInfo

		for _, rssURL := range rssFeeds {
			rssURL = normalizeURL(rssURL)
			fmt.Printf("\n   📰 Основной RSS: %s\n", rssURL)

			// Ищем дополнительные RSS-фиды категорий
			// Ограничиваем глубину до 1 уровня для HTML-страниц, чтобы избежать зацикливания
			categoryRSS := findCategoryRSSFromPage(rssURL, 1)

			// Добавляем основной RSS (он уже помечен как посещенный в findCategoryRSSFromPage)
			// но мы все равно добавляем его в список как "main"
			siteRSSList = append(siteRSSList, RSSFeedInfo{
				URL:    rssURL,
				Type:   "main",
				Source: site.ID,
			})

			// Добавляем найденные RSS категорий (исключая основной RSS, если он там есть)
			for catRSS := range categoryRSS {
				// Пропускаем основной RSS, если он попал в categoryRSS
				if catRSS == rssURL {
					continue
				}
				// Проверяем, не добавлен ли уже (может быть добавлен ранее)
				alreadyAdded := false
				for _, existing := range siteRSSList {
					if existing.URL == catRSS {
						alreadyAdded = true
						break
					}
				}
				if !alreadyAdded {
					siteRSSList = append(siteRSSList, RSSFeedInfo{
						URL:    catRSS,
						Type:   "category",
						Source: site.ID,
					})
				}
			}
		}

		allFoundRSS = append(allFoundRSS, siteRSSList...)
		fmt.Printf("   ✅ Всего найдено RSS-фидов для %s: %d\n", site.Name, len(siteRSSList))
	}

	// Выводим результаты
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("\n📊 ИТОГО: Найдено %d уникальных RSS-фидов\n\n", len(allFoundRSS))

	// Группируем по сайтам
	bySite := make(map[string][]RSSFeedInfo)
	for _, rss := range allFoundRSS {
		bySite[rss.Source] = append(bySite[rss.Source], rss)
	}

	// Выводим структурированный список
	fmt.Println("📝 Найденные RSS-фиды по сайтам:")
	fmt.Println()
	for siteID, rssList := range bySite {
		var mainFeeds, categoryFeeds []RSSFeedInfo
		for _, rss := range rssList {
			if rss.Type == "main" {
				mainFeeds = append(mainFeeds, rss)
			} else {
				categoryFeeds = append(categoryFeeds, rss)
			}
		}

		fmt.Printf("  %s:\n", siteID)
		fmt.Printf("    Основные RSS (%d):\n", len(mainFeeds))
		for _, rss := range mainFeeds {
			fmt.Printf("      - %s\n", rss.URL)
		}

		if len(categoryFeeds) > 0 {
			fmt.Printf("    RSS категорий (%d):\n", len(categoryFeeds))
			for _, rss := range categoryFeeds {
				fmt.Printf("      - %s\n", rss.URL)
			}
		}
		fmt.Println()
	}

	// Выводим YAML-формат для обновления sites.yaml
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println()
	fmt.Println("💡 YAML-формат для обновления sites.yaml:")
	fmt.Println()
	fmt.Println("sites:")
	for siteID, rssList := range bySite {
		var siteInfo *SiteConfig
		for i := range config.Sites {
			if config.Sites[i].ID == siteID {
				siteInfo = &config.Sites[i]
				break
			}
		}
		if siteInfo == nil {
			continue
		}

		fmt.Printf("  - id: \"%s\"\n", siteID)
		fmt.Printf("    name: \"%s\"\n", siteInfo.Name)
		fmt.Printf("    url: \"%s\"\n", siteInfo.URL)
		fmt.Printf("    rss_feeds:\n")

		// Сортируем: сначала main, потом category, потом по URL
		sort.Slice(rssList, func(i, j int) bool {
			if rssList[i].Type != rssList[j].Type {
				return rssList[i].Type == "main"
			}
			return rssList[i].URL < rssList[j].URL
		})

		for _, rss := range rssList {
			fmt.Printf("      - \"%s\"\n", rss.URL)
		}
		fmt.Printf("    priority: %d\n", siteInfo.Priority)
		fmt.Println()
	}

	// Сохраняем в файл
	outputFile := "scripts/found_rss_feeds.yaml"
	outputConfig := OutputConfig{
		Sites: make([]OutputSiteConfig, 0, len(bySite)),
	}

	for siteID, rssList := range bySite {
		var siteInfo *SiteConfig
		for i := range config.Sites {
			if config.Sites[i].ID == siteID {
				siteInfo = &config.Sites[i]
				break
			}
		}
		if siteInfo == nil {
			continue
		}

		// Сортируем RSS-фиды
		sort.Slice(rssList, func(i, j int) bool {
			if rssList[i].Type != rssList[j].Type {
				return rssList[i].Type == "main"
			}
			return rssList[i].URL < rssList[j].URL
		})

		rssURLs := make([]string, len(rssList))
		for i, rss := range rssList {
			rssURLs[i] = rss.URL
		}

		outputConfig.Sites = append(outputConfig.Sites, OutputSiteConfig{
			ID:       siteID,
			Name:     siteInfo.Name,
			URL:      siteInfo.URL,
			RSSFeeds: rssURLs,
			Priority: siteInfo.Priority,
		})
	}

	// Сортируем сайты по ID для консистентности
	sort.Slice(outputConfig.Sites, func(i, j int) bool {
		return outputConfig.Sites[i].ID < outputConfig.Sites[j].ID
	})

	outputData, err := yaml.Marshal(outputConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Ошибка при формировании YAML: %v\n", err)
		os.Exit(1)
	}

	header := "# Автоматически найденные RSS-фиды\n# Сгенерировано скриптом cmd/scrape-rss/main.go\n\n"
	if err := os.WriteFile(outputFile, []byte(header+string(outputData)), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Не удалось сохранить в файл: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("💾 Результаты сохранены в %s\n", outputFile)
}
