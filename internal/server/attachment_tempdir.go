package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const attachmentTempDirEnv = "CHATBANG_ATTACHMENT_TMPDIR"

// materializeAttachments uses os.MkdirTemp("", ...), which resolves through
// TMPDIR on Unix. Chrome builds installed through Snap or otherwise sandboxed can
// have a private /tmp namespace: CDP accepts the selected pathname, but Chrome
// sees an empty File because the ChatBang /tmp path is not the browser's /tmp.
//
// Keep attachment staging in a non-hidden directory under the user's home by
// default so both ChatBang and a sandboxed desktop Chrome can see the same path.
// Users with a remote/container browser can point CHATBANG_ATTACHMENT_TMPDIR at a
// filesystem path shared by both processes.
func init() {
	root := strings.TrimSpace(os.Getenv(attachmentTempDirEnv))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			fmt.Fprintf(os.Stderr, "[server] warning: cannot resolve home directory for attachment staging: %v\n", err)
			return
		}
		root = filepath.Join(home, "chatbang-uploads")
	}

	root, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[server] warning: cannot resolve attachment temp root %q: %v\n", root, err)
		return
	}
	if err := os.MkdirAll(root, 0o711); err != nil {
		fmt.Fprintf(os.Stderr, "[server] warning: cannot create attachment temp root %q: %v\n", root, err)
		return
	}
	// MkdirAll does not widen permissions on an existing directory. Ensure the
	// browser can traverse this dedicated staging root without making it listable.
	if err := os.Chmod(root, 0o711); err != nil {
		fmt.Fprintf(os.Stderr, "[server] warning: cannot make attachment temp root traversable %q: %v\n", root, err)
		return
	}
	if err := os.Setenv("TMPDIR", root); err != nil {
		fmt.Fprintf(os.Stderr, "[server] warning: cannot set attachment temp root %q: %v\n", root, err)
		return
	}
	fmt.Fprintf(os.Stderr, "[server] attachment temp root=%q\n", root)
}
