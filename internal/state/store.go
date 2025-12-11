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

// LoadDigest читает готовый дайджест из файла.
func (s *FileStore) LoadDigest(ctx context.Context) (*news.Digest, error) {
	digestPath := s.path[:len(s.path)-len("state.json")] + "digest.json"
	data, err := os.ReadFile(digestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Файл не существует - это нормально
		}
		return nil, fmt.Errorf("read digest file: %w", err)
	}

	var digest news.Digest
	if err := json.Unmarshal(data, &digest); err != nil {
		return nil, fmt.Errorf("unmarshal digest: %w", err)
	}

	return &digest, nil
}

// SaveDigest сохраняет готовый дайджест в файл атомарно.
func (s *FileStore) SaveDigest(ctx context.Context, digest *news.Digest) error {
	data, err := json.MarshalIndent(digest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal digest: %w", err)
	}

	digestPath := s.path[:len(s.path)-len("state.json")] + "digest.json"
	dir := filepath.Dir(digestPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create digest directory: %w", err)
	}

	// Атомарная запись через временный файл
	tmpPath := digestPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp digest file: %w", err)
	}

	// Переименовываем временный файл - это атомарная операция
	if err := os.Rename(tmpPath, digestPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp digest file: %w", err)
	}

	return nil
}

// DeleteDigest удаляет файл дайджеста.
func (s *FileStore) DeleteDigest(ctx context.Context) error {
	digestPath := s.path[:len(s.path)-len("state.json")] + "digest.json"
	if err := os.Remove(digestPath); err != nil {
		if os.IsNotExist(err) {
			return nil // Файл не существует - это нормально
		}
		return fmt.Errorf("delete digest file: %w", err)
	}
	return nil
}

