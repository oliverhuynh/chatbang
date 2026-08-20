package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAttachmentTempRootUsesExplicitSharedDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shared")
	t.Setenv(attachmentTempDirEnv, root)

	got, err := attachmentTempRoot()
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("attachment temp root = %q, want %q", got, want)
	}
	if info, err := os.Stat(got); err != nil {
		t.Fatalf("stat attachment temp root: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("attachment temp root %q is not a directory", got)
	}
}

func TestAttachmentTempRootDoesNotChangeProcessTMPDIR(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shared")
	t.Setenv(attachmentTempDirEnv, root)
	before := os.Getenv("TMPDIR")

	if _, err := attachmentTempRoot(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TMPDIR"); got != before {
		t.Fatalf("TMPDIR changed from %q to %q", before, got)
	}
}
