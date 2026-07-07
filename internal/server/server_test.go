package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if prompt != "USER: Hello\nworld" {
		t.Fatalf("flattenMessages() = %q", prompt)
	}
}

func TestChatCompletionsHandler(t *testing.T) {
	handler := NewHandler(AskFunc(func(prompt string) (string, error) {
		if !strings.Contains(prompt, "USER: Ping") {
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
