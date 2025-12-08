package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maine/vietnam_bot_news/internal/news"
)

func TestFileStore_Load_Save(t *testing.T) {
	// Создаём временную директорию для тестов
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	store := NewFileStore(statePath)
	ctx := context.Background()

	t.Run("load non-existent file returns empty state", func(t *testing.T) {
		state, err := store.Load(ctx)
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if state.LastRun.IsZero() == false {
			t.Errorf("Load() LastRun should be zero")
		}
		if len(state.SentArticles) != 0 {
			t.Errorf("Load() SentArticles should be empty")
		}
	})

	t.Run("save and load state", func(t *testing.T) {
		now := time.Date(2024, 12, 3, 12, 0, 0, 0, time.UTC)
		state := news.State{
			LastRun: now,
			SentArticles: []news.StateArticle{
				{ID: "article-1", SentAt: now},
				{ID: "article-2", SentAt: now},
			},
			Recipients: []news.RecipientBinding{
				{Name: "user1", ChatID: "123", UpdatedAt: now},
			},
			Telegram: news.TelegramState{
				LastUpdateID: 100,
			},
		}

		if err := store.Save(ctx, state); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		loaded, err := store.Load(ctx)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if !loaded.LastRun.Equal(state.LastRun) {
			t.Errorf("Load() LastRun = %v, want %v", loaded.LastRun, state.LastRun)
		}
		if len(loaded.SentArticles) != len(state.SentArticles) {
			t.Errorf("Load() SentArticles len = %v, want %v", len(loaded.SentArticles), len(state.SentArticles))
		}
		if len(loaded.Recipients) != len(state.Recipients) {
			t.Errorf("Load() Recipients len = %v, want %v", len(loaded.Recipients), len(state.Recipients))
		}
		if loaded.Telegram.LastUpdateID != state.Telegram.LastUpdateID {
			t.Errorf("Load() LastUpdateID = %v, want %v", loaded.Telegram.LastUpdateID, state.Telegram.LastUpdateID)
		}
	})

	t.Run("load corrupted JSON returns empty state", func(t *testing.T) {
		// Создаём повреждённый JSON файл
		corruptedPath := filepath.Join(tmpDir, "corrupted.json")
		corruptedStore := NewFileStore(corruptedPath)
		if err := os.WriteFile(corruptedPath, []byte("invalid json {"), 0644); err != nil {
			t.Fatalf("failed to write corrupted file: %v", err)
		}

		state, err := corruptedStore.Load(ctx)
		if err != nil {
			t.Fatalf("Load() should not return error for corrupted JSON, got %v", err)
		}
		if !state.LastRun.IsZero() {
			t.Errorf("Load() should return empty state for corrupted JSON")
		}

		// Проверяем, что повреждённый файл сохранён
		if _, err := os.Stat(corruptedPath + ".broken"); os.IsNotExist(err) {
			t.Error("Load() should save corrupted file as .broken")
		}
	})

	t.Run("create directory if not exists", func(t *testing.T) {
		nestedPath := filepath.Join(tmpDir, "nested", "path", "state.json")
		nestedStore := NewFileStore(nestedPath)

		state := news.State{LastRun: time.Now()}
		if err := nestedStore.Save(ctx, state); err != nil {
			t.Fatalf("Save() should create directory, error = %v", err)
		}

		if _, err := os.Stat(nestedPath); os.IsNotExist(err) {
			t.Error("Save() should create nested directory")
		}
	})
}

func TestFileStore_Save_Atomic(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "atomic.json")
	store := NewFileStore(statePath)
	ctx := context.Background()

	state := news.State{
		LastRun: time.Now(),
		SentArticles: []news.StateArticle{
			{ID: "test", SentAt: time.Now()},
		},
	}

	// Сохраняем state
	if err := store.Save(ctx, state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Проверяем, что файл существует и временный файл удалён
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Error("Save() should create state file")
	}
	if _, err := os.Stat(statePath + ".tmp"); err == nil {
		t.Error("Save() should remove temporary file")
	}
}

