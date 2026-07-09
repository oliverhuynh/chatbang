package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestFlattenMessages(t *testing.T) {
	prompt, err := flattenMessages([]chatRequestMessage{
		{Role: "system", Content: json.RawMessage(`"You are terse."`)},
		{Role: "user", Content: json.RawMessage(`"Say hi"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "SYSTEM: You are terse.\n\nUSER: Say hi"
	if prompt != want {
		t.Fatalf("flattenMessages() = %q, want %q", prompt, want)
	}
}

func TestFlattenMessagesSupportsTextParts(t *testing.T) {
	prompt, err := flattenMessages([]chatRequestMessage{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"Hello"},{"type":"input_image","image_url":"x"},{"type":"text","text":"world"}]`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "Hello\nworld" {
		t.Fatalf("flattenMessages() = %q", prompt)
	}
}

func TestFlattenMessagesSingleUserMessagePassesThrough(t *testing.T) {
	prompt, err := flattenMessages([]chatRequestMessage{
		{Role: "user", Content: json.RawMessage(`"Say hi"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "Say hi" {
		t.Fatalf("flattenMessages() = %q", prompt)
	}
}

func TestChatCompletionsHandler(t *testing.T) {
	handler := NewHandler(AskFunc(func(prompt string) (string, error) {
		if prompt != "Ping" {
			t.Fatalf("prompt = %q", prompt)
		}
		return "Pong", nil
	}))

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	rec := httptest.NewRecorder()

	mux := http.NewServeMux()
	handler.Register(mux)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp chatCompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Object != "chat.completion" {
		t.Fatalf("object = %q", resp.Object)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "Pong" {
		t.Fatalf("choices = %+v", resp.Choices)
	}
}

func TestChatCompletionsRejectsStream(t *testing.T) {
	handler := NewHandler(AskFunc(func(prompt string) (string, error) {
		return "unused", nil
	}))

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"Ping"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	rec := httptest.NewRecorder()

	handler.handleChatCompletions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestModelsHandler(t *testing.T) {
	handler := NewHandler(AskFunc(func(prompt string) (string, error) {
		t.Fatalf("asker should not be called")
		return "", nil
	}))

	for _, path := range []string{"/v1/models", "/models"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		mux := http.NewServeMux()
		handler.Register(mux)
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}

		var resp modelsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s unmarshal: %v", path, err)
		}
		if resp.Object != "list" {
			t.Fatalf("%s object = %q", path, resp.Object)
		}
		if len(resp.Data) == 0 {
			t.Fatalf("%s data empty", path)
		}
		if resp.Data[0].Object != "model" {
			t.Fatalf("%s first object = %q", path, resp.Data[0].Object)
		}
	}
}

func TestQuotedPreviewPreservesSpaces(t *testing.T) {
	got := quotedPreview("  hi  \nthere\t", 100)
	want := "\"  hi  \\nthere\\t\""
	if got != want {
		t.Fatalf("quotedPreview() = %q, want %q", got, want)
	}
}

func TestChatCompletionsLogsIncomingPrompt(t *testing.T) {
	handler := NewHandler(AskFunc(func(prompt string) (string, error) {
		return "Pong", nil
	}))

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
	}()

	body := bytes.NewBufferString(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"  Ping  "}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	rec := httptest.NewRecorder()

	handler.handleChatCompletions(rec, req)
	_ = w.Close()
	logBytes, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	logOutput := string(logBytes)
	if !strings.Contains(logOutput, `[server] incoming messages: "[{\"role\":\"user\",\"content\":\"  Ping  \"}]"`) {
		t.Fatalf("log output = %q", logOutput)
	}
}
