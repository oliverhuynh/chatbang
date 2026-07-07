package session

import (
	"path/filepath"
	"testing"
)

func TestSaveAndResolveSessionSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")

	if err := SaveSessionSnapshot(path, "https://chatgpt.com/c/first", "First chat"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSessionSnapshot(path, "https://chatgpt.com/c/second", "Second chat"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSessionSnapshot(path, "https://chatgpt.com/c/first", "First chat updated"); err != nil {
		t.Fatal(err)
	}

	sessions, err := ListSavedSessions(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	if sessions[1].Title != "First chat updated" {
		t.Fatalf("got updated title %q", sessions[1].Title)
	}

	last, err := ResolveSavedSession(path, "last")
	if err != nil {
		t.Fatal(err)
	}
	if last.URL != "https://chatgpt.com/c/second" {
		t.Fatalf("got last URL %q", last.URL)
	}

	got, err := ResolveSavedSession(path, sessions[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://chatgpt.com/c/first" {
		t.Fatalf("got URL %q", got.URL)
	}
}

func TestSaveSessionSnapshotSkipsNonConversationURLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")

	if err := SaveSessionSnapshot(path, "https://chatgpt.com/", "Home"); err != nil {
		t.Fatal(err)
	}

	sessions, err := ListSavedSessions(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("got %d sessions, want 0", len(sessions))
	}
}
