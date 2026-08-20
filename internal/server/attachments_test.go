package server

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestNormalizeMessageContentImageURL(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"text","text":"Describe this image"},
		{"type":"image_url","image_url":{"url":"data:image/png;base64,aGk="}}
	]`)

	text, specs, err := normalizeMessageContent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Describe this image\n[Attached image: image]" {
		t.Fatalf("text = %q", text)
	}
	if len(specs) != 1 || specs[0].Kind != "image" || !strings.HasPrefix(specs[0].Source, "data:image/png") {
		t.Fatalf("specs = %+v", specs)
	}
}

func TestNormalizeMessageContentChatFile(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"text","text":"Summarize the file"},
		{"type":"file","file":{"filename":"notes.txt","file_data":"aGVsbG8="}}
	]`)

	text, specs, err := normalizeMessageContent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Summarize the file\n[Attached file: notes.txt]" {
		t.Fatalf("text = %q", text)
	}
	if len(specs) != 1 || specs[0].Filename != "notes.txt" || specs[0].Data != "aGVsbG8=" {
		t.Fatalf("specs = %+v", specs)
	}
}

func TestNormalizeMessageContentResponsesInputFile(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"input_text","text":"Review this"},
		{"type":"input_file","filename":"report.md","file_data":"IyBSZXBvcnQ="}
	]`)

	text, specs, err := normalizeMessageContent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Review this\n[Attached file: report.md]" {
		t.Fatalf("text = %q", text)
	}
	if len(specs) != 1 || specs[0].Filename != "report.md" {
		t.Fatalf("specs = %+v", specs)
	}
}

func TestMaterializeAttachmentBase64(t *testing.T) {
	files, cleanup, err := materializeAttachments(context.Background(), []attachmentSpec{{
		Kind:     "file",
		Filename: "notes.txt",
		Data:     "aGVsbG8=",
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if len(files) != 1 {
		t.Fatalf("files = %+v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q", data)
	}
	if got := strings.TrimSpace(files[0]); !strings.HasSuffix(got, "notes.txt") {
		t.Fatalf("path = %q", got)
	}
}

func TestMaterializeAttachmentDataURL(t *testing.T) {
	files, cleanup, err := materializeAttachments(context.Background(), []attachmentSpec{{
		Kind:   "image",
		Source: "data:image/png;base64,aGVsbG8=",
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if len(files) != 1 {
		t.Fatalf("files = %+v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q", data)
	}
}

func TestMaterializeAttachmentRejectsFileID(t *testing.T) {
	_, cleanup, err := materializeAttachments(context.Background(), []attachmentSpec{{
		Kind:   "file",
		FileID: "file-abc123",
	}})
	defer cleanup()
	if err == nil || !strings.Contains(err.Error(), "cannot be resolved") {
		t.Fatalf("err = %v", err)
	}
}

func TestMaterializeAttachmentRejectsPrivateURL(t *testing.T) {
	_, cleanup, err := materializeAttachments(context.Background(), []attachmentSpec{{
		Kind:   "file",
		Source: "http://127.0.0.1/private.txt",
	}})
	defer cleanup()
	if err == nil || !strings.Contains(err.Error(), "private or unsafe") {
		t.Fatalf("err = %v", err)
	}
}

func TestNormalizeMessagesDeduplicatesAttachments(t *testing.T) {
	content := json.RawMessage(`[
		{"type":"text","text":"Look"},
		{"type":"image_url","image_url":{"url":"data:image/png;base64,aGk="}}
	]`)
	messages := []chatRequestMessage{
		{Role: "user", Content: content},
		{Role: "user", Content: content},
	}

	_, specs, err := normalizeMessagesAndAttachments(messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("specs = %+v", specs)
	}
}

type attachmentRecordingAsker struct {
	prompt string
	files  []string
}

func (a *attachmentRecordingAsker) AskFresh(prompt string) (string, error) {
	a.prompt = prompt
	return "text-only", nil
}

func (a *attachmentRecordingAsker) AskFreshWithFiles(prompt string, files []string) (string, error) {
	a.prompt = prompt
	a.files = append([]string(nil), files...)
	return "with-files", nil
}

func TestAskFreshWithFilesUsesAttachmentCapability(t *testing.T) {
	a := &attachmentRecordingAsker{}
	got, err := askFreshWithFiles(a, "prompt", []string{"/tmp/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "with-files" || a.prompt != "prompt" || len(a.files) != 1 {
		t.Fatalf("got=%q prompt=%q files=%+v", got, a.prompt, a.files)
	}
}
