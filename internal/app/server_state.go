package app

import (
	"encoding/json"
	"errors"
	"os"
	"syscall"

	"github.com/KaraBala10/chatbang-pro/internal/cli"
	"github.com/KaraBala10/chatbang-pro/internal/config"
)

const standaloneBlockedMessage = "Server is running, standalone commands is not allowed. Please switch to use OpenAI compatible call"

type serverState struct {
	PID        int    `json:"pid"`
	ListenAddr string `json:"listen_addr"`
}

func shouldBlockForRunningServer(paths config.Paths, opts cli.Options) (bool, error) {
	if opts.ServerMode || opts.KillBrowser || opts.WantConfig || opts.WantHelp || opts.ListSessions {
		return false, nil
	}

	state, err := readServerState(paths.ServerState)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if state.PID <= 0 {
		return false, nil
	}
	if !processAlive(state.PID) {
		_ = os.Remove(paths.ServerState)
		return false, nil
	}
	return true, nil
}

func readServerState(path string) (serverState, error) {
	var state serverState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func writeServerState(path, listenAddr string) error {
	data, err := json.Marshal(serverState{
		PID:        os.Getpid(),
		ListenAddr: listenAddr,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
