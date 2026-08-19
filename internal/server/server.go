package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type asker interface {
	AskFresh(prompt string) (string, error)
}

type Handler struct {
	asker asker
}

type chatCompletionsRequest struct {
	Model    string               `json:"model"`
	Messages []chatRequestMessage `json:"messages"`
	Stream   bool                 `json:"stream"`
}

type chatRequestMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type chatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []chatCompletionChoice `json:"choices"`
	Usage   chatCompletionUsage    `json:"usage"`
}

type chatCompletionChunk struct {
	ID      string                      `json:"id"`
	Object  string                      `json:"object"`
	Created int64                       `json:"created"`
	Model   string                      `json:"model"`
	Choices []chatCompletionChunkChoice `json:"choices"`
}

type chatCompletionChunkChoice struct {
	Index        int                 `json:"index"`
	Delta        chatCompletionDelta `json:"delta"`
	FinishReason *string             `json:"finish_reason"`
}

type chatCompletionDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type modelsResponse struct {
	Object string      `json:"object"`
	Data   []modelInfo `json:"data"`
}

type modelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type chatCompletionChoice struct {
	Index        int                   `json:"index"`
	Message      chatCompletionMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type chatCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    any    `json:"code"`
}

func NewHandler(a asker) *Handler {
	return &Handler{asker: a}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/v1/chat/completions", h.handleChatCompletions)
	mux.HandleFunc("/v1/models", h.handleModels)
	mux.HandleFunc("/models", h.handleModels)
}

var supportedModels = []string{
	"gpt-4o",
	"gpt-4o-mini",
	"gpt-4.1",
	"gpt-4.1-mini",
}

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request_error", "", "method not allowed")
		return
	}

	var req chatCompletionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "model", "model is required")
		return
	}
	if payload, err := json.Marshal(req.Messages); err == nil {
		fmt.Fprintf(os.Stderr, "[server] incoming messages: %s\n", quotedPreview(string(payload), 800))
	}
	prompt, err := flattenMessages(req.Messages)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "messages", err.Error())
		return
	}
	fmt.Fprintf(os.Stderr, "[server] flattened prompt nl=%d: %s\n", strings.Count(prompt, "\n"), quotedPreview(prompt, 800))

	reply, err := h.asker.AskFresh(prompt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "", err.Error())
		return
	}

	if req.Stream {
		writeChatCompletionStream(w, req.Model, reply)
		return
	}

	resp := chatCompletionResponse{
		ID:      newID("chatcmpl"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []chatCompletionChoice{{
			Index: 0,
			Message: chatCompletionMessage{
				Role:    "assistant",
				Content: reply,
			},
			FinishReason: "stop",
		}},
		Usage: chatCompletionUsage{},
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request_error", "", "method not allowed")
		return
	}

	models := make([]modelInfo, 0, len(supportedModels))
	for _, id := range supportedModels {
		models = append(models, modelInfo{
			ID:      id,
			Object:  "model",
			Created: 0,
			OwnedBy: "openai",
		})
	}

	writeJSON(w, http.StatusOK, modelsResponse{
		Object: "list",
		Data:   models,
	})
}

func flattenMessages(messages []chatRequestMessage) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("messages must not be empty")
	}

	systemParts := make([]string, 0, 1)
	conversationLines := make([]string, 0, len(messages))
	var finalUser string
	for i, message := range messages {
		role := strings.TrimSpace(message.Role)
		if role == "" {
			return "", fmt.Errorf("message role is required")
		}
		fmt.Fprintf(os.Stderr, "[server] message[%d] role=%q raw-content: %s\n", i, role, quotedPreview(string(message.Content), 400))
		content, err := extractContentText(message.Content)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(os.Stderr, "[server] message[%d] extracted nl=%d: %s\n", i, strings.Count(content, "\n"), quotedPreview(content, 400))
		trimmed := strings.TrimSpace(content)
		fmt.Fprintf(os.Stderr, "[server] message[%d] trimmed nl=%d: %s\n", i, strings.Count(trimmed, "\n"), quotedPreview(trimmed, 400))
		if trimmed == "" {
			continue
		}
		switch strings.ToLower(role) {
		case "system":
			systemParts = append(systemParts, trimmed)
		case "user":
			if finalUser != "" {
				conversationLines = append(conversationLines, "User:\n"+finalUser)
			}
			finalUser = trimmed
		case "assistant":
			if finalUser != "" {
				conversationLines = append(conversationLines, "User:\n"+finalUser)
				finalUser = ""
			}
			conversationLines = append(conversationLines, "Assistant:\n"+trimmed)
		default:
			if finalUser != "" {
				conversationLines = append(conversationLines, "User:\n"+finalUser)
				finalUser = ""
			}
			conversationLines = append(conversationLines, strings.ToUpper(role)+":\n"+trimmed)
		}
	}
	if len(systemParts) == 0 && len(conversationLines) == 0 && finalUser == "" {
		return "", fmt.Errorf("messages must include text content")
	}

	sections := make([]string, 0, 3)
	if len(systemParts) > 0 {
		sections = append(sections, "# System\n\n"+strings.Join(systemParts, "\n\n"))
	}
	if len(conversationLines) > 0 {
		sections = append(sections, "# Conversation\n\n"+strings.Join(conversationLines, "\n\n"))
	}
	if finalUser != "" {
		sections = append(sections, "# User\n\n"+finalUser)
	}
	return strings.Join(sections, "\n\n"), nil
}

func extractContentText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", fmt.Errorf("message content must be a string or text parts array")
	}

	var chunks []string
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			chunks = append(chunks, part.Text)
		}
	}
	return strings.Join(chunks, "\n"), nil
}

func writeChatCompletionStream(w http.ResponseWriter, model, reply string) {
	id := newID("chatcmpl")
	created := time.Now().Unix()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	writeChunk := func(delta chatCompletionDelta, finishReason *string) {
		chunk := chatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []chatCompletionChunkChoice{{
				Index:        0,
				Delta:        delta,
				FinishReason: finishReason,
			}},
		}
		payload, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}

	writeChunk(chatCompletionDelta{Role: "assistant"}, nil)
	if reply != "" {
		writeChunk(chatCompletionDelta{Content: reply}, nil)
	}
	finishReason := "stop"
	writeChunk(chatCompletionDelta{}, &finishReason)
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func newID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return prefix + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

func quotedPreview(text string, limit int) string {
	if limit <= 0 {
		limit = 1
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return fmt.Sprintf("%q", text)
	}
	return fmt.Sprintf("%q", string(runes[:limit])+"...")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, errType, param, message string) {
	writeJSON(w, status, errorResponse{
		Error: apiError{
			Message: message,
			Type:    errType,
			Param:   param,
			Code:    nil,
		},
	})
}

type AskFunc func(prompt string) (string, error)

func (f AskFunc) AskFresh(prompt string) (string, error) {
	return f(prompt)
}
