package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/KaraBala10/chatbang-pro/internal/cli"
	"github.com/KaraBala10/chatbang-pro/internal/config"
)

func TestShouldBlockForRunningServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	if err := writeServerState(path, "127.0.0.1:19999"); err != nil {
		t.Fatal(err)
	}

	blocked, err := shouldBlockForRunningServer(config.Paths{ServerState: path}, cli.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Fatal("expected standalone call to be blocked while server state is active")
	}
}

func TestShouldBlockForRunningServerIgnoresServerMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	if err := writeServerState(path, "127.0.0.1:19999"); err != nil {
		t.Fatal(err)
	}

	blocked, err := shouldBlockForRunningServer(config.Paths{ServerState: path}, cli.Options{ServerMode: true})
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("server mode should not block itself")
	}
}

func TestShouldBlockForRunningServerRemovesStaleState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(path, []byte(`{"pid":999999,"listen_addr":"127.0.0.1:19999"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	blocked, err := shouldBlockForRunningServer(config.Paths{ServerState: path}, cli.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("stale state should not block standalone calls")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected stale state file to be removed, stat err=%v", err)
	}
}
