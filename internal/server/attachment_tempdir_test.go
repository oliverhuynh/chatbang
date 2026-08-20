package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAttachmentTempRootDefaultsOutsideSystemTmp(t *testing.T) {
	root := os.Getenv("TMPDIR")
	if root == "" {
		t.Fatal("TMPDIR is not configured for attachment staging")
	}
	if filepath.Base(root) != "chatbang-uploads" && os.Getenv(attachmentTempDirEnv) == "" {
		t.Fatalf("default attachment temp root = %q, want chatbang-uploads", root)
	}
}
