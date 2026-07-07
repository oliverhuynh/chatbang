package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SavedSession struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	UpdatedAt string `json:"updated_at"`
}

type savedSessions struct {
	Sessions []SavedSession `json:"sessions"`
}

func loadSavedSessions(path string) ([]SavedSession, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var store savedSessions
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return store.Sessions, nil
}

func saveSessions(path string, sessions []SavedSession) error {
	return writeJSON(path, savedSessions{Sessions: sessions})
}

func SaveSessionSnapshot(path, url, title string) error {
	url = strings.TrimSpace(url)
	if !strings.Contains(url, "chatgpt.com") || !strings.Contains(url, "/c/") {
		return nil
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "ChatGPT session"
	}

	now := time.Now().Format(time.RFC3339)
	sessions, err := loadSavedSessions(path)
	if err != nil {
		return err
	}

	for i := range sessions {
		if sessions[i].URL == url {
			sessions[i].Title = title
			sessions[i].UpdatedAt = now
			return saveSessions(path, sessions)
		}
	}

	id := time.Now().UTC().Format("20060102-150405.000000000")
	sessions = append([]SavedSession{{
		ID:        id,
		Title:     title,
		URL:       url,
		UpdatedAt: now,
	}}, sessions...)
	return saveSessions(path, sessions)
}

func ResolveSavedSession(path, key string) (SavedSession, error) {
	sessions, err := loadSavedSessions(path)
	if err != nil {
		return SavedSession{}, err
	}
	if len(sessions) == 0 {
		return SavedSession{}, fmt.Errorf("no saved sessions")
	}
	key = strings.TrimSpace(key)
	if key == "" || key == "last" {
		return sessions[0], nil
	}
	for _, item := range sessions {
		if item.ID == key || strings.HasPrefix(item.ID, key) {
			return item, nil
		}
	}
	return SavedSession{}, fmt.Errorf("saved session %q not found", key)
}

func ListSavedSessions(path string) ([]SavedSession, error) {
	return loadSavedSessions(path)
}

func writeJSON(path string, value any) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
