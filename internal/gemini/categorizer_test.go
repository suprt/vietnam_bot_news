package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/maine/vietnam_bot_news/internal/config"
	"github.com/maine/vietnam_bot_news/internal/news"
)

// mockGeminiClient - мок для тестирования Categorizer
type mockGeminiClient struct {
	generateTextFunc func(ctx context.Context, model string, prompt string) (string, error)
}

func (m *mockGeminiClient) GenerateText(ctx context.Context, model string, prompt string) (string, error) {
	if m.generateTextFunc != nil {
		return m.generateTextFunc(ctx, model, prompt)
	}
	return "", errors.New("not implemented")
}

func TestCategorizer_Categorize(t *testing.T) {
	cfg := config.Gemini{
		ModelCategorization:   "models/gemini-2.5-flash",
		BatchSizeCategorization: 15,
	}
	pipelineCfg := config.Pipeline{
		Categories: []string{
			"Политика",
			"Экономика и бизнес",
			"Другое / Разное",
		},
	}

	tests := []struct {
		name     string
		articles []news.ArticleRaw
		mockFunc func(ctx context.Context, model string, prompt string) (string, error)
		wantErr  bool
		wantLen  int
	}{
		{
			name:     "empty articles",
			articles: []news.ArticleRaw{},
			mockFunc: nil,
			wantErr:  false,
			wantLen:  0,
		},
		{
			name: "successful categorization",
			articles: []news.ArticleRaw{
				{
					ID:         "article-1",
					Title:      "Political news",
					RawContent: "Some political content here",
				},
				{
					ID:         "article-2",
					Title:      "Economic news",
					RawContent: "Some economic content here",
				},
			},
			mockFunc: func(ctx context.Context, model string, prompt string) (string, error) {
				response := []categoryResponse{
					{ID: "article-1", Category: "Политика"},
					{ID: "article-2", Category: "Экономика и бизнес"},
				}
				data, _ := json.Marshal(response)
				return string(data), nil
			},
			wantErr: false,
			wantLen: 2,
		},
		{
			name: "invalid category falls back to default",
			articles: []news.ArticleRaw{
				{
					ID:         "article-1",
					Title:      "Some news",
					RawContent: "Some content",
				},
			},
			mockFunc: func(ctx context.Context, model string, prompt string) (string, error) {
				response := []categoryResponse{
					{ID: "article-1", Category: "Invalid Category"},
				}
				data, _ := json.Marshal(response)
				return string(data), nil
			},
			wantErr: false,
			wantLen: 1,
		},
		{
			name: "missing article in response uses fallback",
			articles: []news.ArticleRaw{
				{
					ID:         "article-1",
					Title:      "Some news",
					RawContent: "Some content",
				},
				{
					ID:         "article-2",
					Title:      "Other news",
					RawContent: "Other content",
				},
			},
			mockFunc: func(ctx context.Context, model string, prompt string) (string, error) {
				// Gemini вернул только одну категорию
				response := []categoryResponse{
					{ID: "article-1", Category: "Политика"},
				}
				data, _ := json.Marshal(response)
				return string(data), nil
			},
			wantErr: false,
			wantLen: 2, // Обе статьи должны быть в результате (вторая с fallback категорией)
		},
		{
			name: "gemini api error",
			articles: []news.ArticleRaw{
				{
					ID:         "article-1",
					Title:      "Some news",
					RawContent: "Some content",
				},
			},
			mockFunc: func(ctx context.Context, model string, prompt string) (string, error) {
				return "", errors.New("api error")
			},
			wantErr: true,
		},
		{
			name: "invalid json response",
			articles: []news.ArticleRaw{
				{
					ID:         "article-1",
					Title:      "Some news",
					RawContent: "Some content",
				},
			},
			mockFunc: func(ctx context.Context, model string, prompt string) (string, error) {
				return "not json", nil
			},
			wantErr: true,
		},
		{
			name: "json with extra text",
			articles: []news.ArticleRaw{
				{
					ID:         "article-1",
					Title:      "Some news",
					RawContent: "Some content",
				},
			},
			mockFunc: func(ctx context.Context, model string, prompt string) (string, error) {
				response := []categoryResponse{
					{ID: "article-1", Category: "Политика"},
				}
				data, _ := json.Marshal(response)
				return "Here is the response: " + string(data) + " End of response", nil
			},
			wantErr: false,
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockGeminiClient{
				generateTextFunc: tt.mockFunc,
			}
			categorizer := NewCategorizer(mockClient, cfg, pipelineCfg)

			ctx := context.Background()
			result, err := categorizer.Categorize(ctx, tt.articles)

			if (err != nil) != tt.wantErr {
				t.Errorf("Categorize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(result) != tt.wantLen {
					t.Errorf("Categorize() len = %v, want %v", len(result), tt.wantLen)
				}

				// Проверяем, что все статьи имеют категории
				for _, catArticle := range result {
					if catArticle.Category == "" {
						t.Errorf("Categorize() article %s has empty category", catArticle.Article.ID)
					}
				}
			}
		})
	}
}

func TestCategorizer_extractJSON(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "pure json",
			text: `[{"id":"1","category":"Политика"}]`,
			want: `[{"id":"1","category":"Политика"}]`,
		},
		{
			name: "json with prefix",
			text: `Here is the response: [{"id":"1","category":"Политика"}]`,
			want: `[{"id":"1","category":"Политика"}]`,
		},
		{
			name: "json with suffix",
			text: `[{"id":"1","category":"Политика"}] End of response`,
			want: `[{"id":"1","category":"Политика"}]`,
		},
		{
			name: "json with both",
			text: `Prefix [{"id":"1","category":"Политика"}] Suffix`,
			want: `[{"id":"1","category":"Политика"}]`,
		},
		{
			name: "no json",
			text: `Just text without json`,
			want: ``,
		},
		{
			name: "nested json",
			text: `Response: [{"id":"1","category":"Политика","data":{"nested":"value"}}]`,
			want: `[{"id":"1","category":"Политика","data":{"nested":"value"}}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.text)
			if got != tt.want {
				t.Errorf("extractJSON() = %v, want %v", got, tt.want)
			}
		})
	}
}
