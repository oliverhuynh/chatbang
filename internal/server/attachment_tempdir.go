package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const attachmentTempDirEnv = "CHATBANG_ATTACHMENT_TMPDIR"

// attachmentTempRoot returns a host path Chrome can resolve as the same file as
// ChatBang. Sandboxed Chromium builds can have a private /tmp namespace, so the
// default is a dedicated non-hidden directory under the user's home.
//
// Remote/container browser setups can override this with
// CHATBANG_ATTACHMENT_TMPDIR, provided the same absolute path is visible to both
// ChatBang and Chrome.
func attachmentTempRoot() (string, error) {
	root := strings.TrimSpace(os.Getenv(attachmentTempDirEnv))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		root = filepath.Join(home, "chatbang-uploads")
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve attachment temp root %q: %w", root, err)
	}
	if err := os.MkdirAll(root, 0o711); err != nil {
		return "", fmt.Errorf("create attachment temp root %q: %w", root, err)
	}
	// MkdirAll keeps the mode of an existing directory. The browser needs
	// traversal permission to reach the per-request directory, but listing the
	// shared root is unnecessary.
	if err := os.Chmod(root, 0o711); err != nil {
		return "", fmt.Errorf("make attachment temp root traversable %q: %w", root, err)
	}
	return root, nil
}
