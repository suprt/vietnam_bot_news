package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/maine/vietnam_bot_news/internal/news"
)

// FileStore хранит состояние в JSON-файле.
type FileStore struct {
	path string
}

// NewFileStore создаёт новый файловый стор.
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

// Load читает состояние из файла.
func (s *FileStore) Load(ctx context.Context) (news.State, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return news.State{}, nil
		}
		return news.State{}, fmt.Errorf("read state file: %w", err)
	}

	var state news.State
	if err := json.Unmarshal(data, &state); err != nil {
		// Fallback: если JSON повреждён, создаём новый пустой state
		// Это позволяет пайплайну продолжить работу даже при повреждённом файле
		// Старый файл будет переименован в .broken для диагностики
		brokenPath := s.path + ".broken"
		_ = os.WriteFile(brokenPath, data, 0644) // Сохраняем повреждённый файл для анализа
		return news.State{}, nil                  // Возвращаем пустой state
	}

	return state, nil
}

// Save записывает состояние в файл атомарно (через временный файл).
func (s *FileStore) Save(ctx context.Context, state news.State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	// Атомарная запись через временный файл
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp state file: %w", err)
	}

	// Переименовываем временный файл - это атомарная операция на большинстве файловых систем
	if err := os.Rename(tmpPath, s.path); err != nil {
		// Если переименование не удалось, пытаемся удалить временный файл
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp state file: %w", err)
	}

	return nil
}

