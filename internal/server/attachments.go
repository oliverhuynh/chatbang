package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxAttachmentBytes = int64(512 * 1024 * 1024)
	maxImageBytes      = int64(20 * 1024 * 1024)
)

type attachmentAsker interface {
	AskFreshWithFiles(prompt string, files []string) (string, error)
}

type attachmentSpec struct {
	Kind     string
	Filename string
	Source   string
	Data     string
	FileID   string
}

type contentPartEnvelope struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ImageURL  json.RawMessage `json:"image_url"`
	File      json.RawMessage `json:"file"`
	FileData  string          `json:"file_data"`
	FileID    string          `json:"file_id"`
	FileURL   string          `json:"file_url"`
	Filename  string          `json:"filename"`
}

type fileContentPart struct {
	FileData string `json:"file_data"`
	FileID   string `json:"file_id"`
	FileURL  string `json:"file_url"`
	Filename string `json:"filename"`
}

func askFreshWithFiles(a asker, prompt string, files []string) (string, error) {
	if len(files) == 0 {
		return a.AskFresh(prompt)
	}
	withFiles, ok := a.(attachmentAsker)
	if !ok {
		return "", fmt.Errorf("attachment upload is not supported by the active ChatBang session")
	}
	return withFiles.AskFreshWithFiles(prompt, files)
}

func prepareMessages(ctx context.Context, messages []chatRequestMessage) (string, []string, func(), error) {
	normalized, specs, err := normalizeMessagesAndAttachments(messages)
	if err != nil {
		return "", nil, func() {}, err
	}
	prompt, err := flattenMessages(normalized)
	if err != nil {
		return "", nil, func() {}, err
	}
	files, cleanup, err := materializeAttachments(ctx, specs)
	if err != nil {
		cleanup()
		return "", nil, func() {}, err
	}
	return prompt, files, cleanup, nil
}

func normalizeMessagesAndAttachments(messages []chatRequestMessage) ([]chatRequestMessage, []attachmentSpec, error) {
	normalized := make([]chatRequestMessage, 0, len(messages))
	attachments := make([]attachmentSpec, 0)
	seen := make(map[string]struct{})

	for _, message := range messages {
		content, specs, err := normalizeMessageContent(message.Content)
		if err != nil {
			return nil, nil, err
		}
		normalized = append(normalized, chatRequestMessage{
			Role:    message.Role,
			Content: mustJSONRawString(content),
		})
		for _, spec := range specs {
			key := attachmentKey(spec)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			attachments = append(attachments, spec)
		}
	}
	return normalized, attachments, nil
}

func normalizeMessageContent(raw json.RawMessage) (string, []attachmentSpec, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil, nil
	}

	var rawParts []json.RawMessage
	if err := json.Unmarshal(raw, &rawParts); err != nil {
		return "", nil, fmt.Errorf("message content must be a string or content parts array")
	}

	chunks := make([]string, 0, len(rawParts))
	attachments := make([]attachmentSpec, 0)
	for _, rawPart := range rawParts {
		var part contentPartEnvelope
		if err := json.Unmarshal(rawPart, &part); err != nil {
			return "", nil, fmt.Errorf("invalid message content part: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(part.Type)) {
		case "text", "input_text", "output_text":
			if strings.TrimSpace(part.Text) != "" {
				chunks = append(chunks, part.Text)
			}
		case "image_url", "input_image":
			source, err := parseImageSource(part.ImageURL)
			if err != nil {
				return "", nil, err
			}
			if source == "" {
				source = strings.TrimSpace(part.FileID)
			}
			if source == "" {
				return "", nil, fmt.Errorf("%s content part requires image_url or file_id", part.Type)
			}
			spec := attachmentSpec{Kind: "image", Source: source, FileID: strings.TrimSpace(part.FileID)}
			attachments = append(attachments, spec)
			chunks = append(chunks, attachmentMarker(spec))
		case "file":
			var file fileContentPart
			if len(part.File) == 0 || string(part.File) == "null" {
				return "", nil, fmt.Errorf("file content part requires a file object")
			}
			if err := json.Unmarshal(part.File, &file); err != nil {
				return "", nil, fmt.Errorf("invalid file content part: %w", err)
			}
			spec := attachmentSpec{
				Kind:     "file",
				Filename: strings.TrimSpace(file.Filename),
				Source:   strings.TrimSpace(file.FileURL),
				Data:     strings.TrimSpace(file.FileData),
				FileID:   strings.TrimSpace(file.FileID),
			}
			if spec.Source == "" && spec.Data == "" && spec.FileID == "" {
				return "", nil, fmt.Errorf("file content part requires file_data, file_url, or file_id")
			}
			attachments = append(attachments, spec)
			chunks = append(chunks, attachmentMarker(spec))
		case "input_file":
			spec := attachmentSpec{
				Kind:     "file",
				Filename: strings.TrimSpace(part.Filename),
				Source:   strings.TrimSpace(part.FileURL),
				Data:     strings.TrimSpace(part.FileData),
				FileID:   strings.TrimSpace(part.FileID),
			}
			if spec.Source == "" && spec.Data == "" && spec.FileID == "" {
				return "", nil, fmt.Errorf("input_file content part requires file_data, file_url, or file_id")
			}
			attachments = append(attachments, spec)
			chunks = append(chunks, attachmentMarker(spec))
		default:
			// Preserve the existing server behavior for unsupported content types:
			// ignore them rather than serializing arbitrary structured payloads into
			// the browser prompt.
		}
	}
	return strings.Join(chunks, "\n"), attachments, nil
}

func parseImageSource(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var direct string
	if err := json.Unmarshal(raw, &direct); err == nil {
		return strings.TrimSpace(direct), nil
	}
	var image struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &image); err != nil {
		return "", fmt.Errorf("image_url must be a string or an object containing url")
	}
	return strings.TrimSpace(image.URL), nil
}

func mustJSONRawString(value string) json.RawMessage {
	payload, _ := json.Marshal(value)
	return payload
}

func attachmentMarker(spec attachmentSpec) string {
	name := strings.TrimSpace(spec.Filename)
	if name == "" {
		name = filenameFromSource(spec.Source)
	}
	if name == "" && spec.FileID != "" {
		name = spec.FileID
	}
	if name == "" {
		name = spec.Kind
	}
	return fmt.Sprintf("[Attached %s: %s]", spec.Kind, name)
}

func attachmentKey(spec attachmentSpec) string {
	base := spec.Kind + "|" + spec.Filename + "|" + spec.Source + "|" + spec.FileID
	if spec.Data == "" {
		return base
	}
	sum := sha256.Sum256([]byte(spec.Data))
	return base + "|" + hex.EncodeToString(sum[:])
}

func materializeAttachments(ctx context.Context, specs []attachmentSpec) ([]string, func(), error) {
	if len(specs) == 0 {
		return nil, func() {}, nil
	}

	var tempDir string
	cleanup := func() {
		if tempDir != "" {
			_ = os.RemoveAll(tempDir)
		}
	}
	ensureTempDir := func() (string, error) {
		if tempDir != "" {
			return tempDir, nil
		}
		dir, err := os.MkdirTemp("", "chatbang-upload-")
		if err != nil {
			return "", err
		}
		tempDir = dir
		return dir, nil
	}

	files := make([]string, 0, len(specs))
	for i, spec := range specs {
		if spec.FileID != "" && spec.Source == "" && spec.Data == "" {
			return nil, cleanup, unresolvedFileIDError(spec.FileID)
		}
		if spec.Source != "" && strings.HasPrefix(spec.Source, "file-") && !strings.Contains(spec.Source, "://") {
			return nil, cleanup, unresolvedFileIDError(spec.Source)
		}

		if localPath, ok, err := resolveLocalAttachment(spec.Source); err != nil {
			return nil, cleanup, err
		} else if ok {
			if err := validateMaterializedFile(localPath, limitForAttachment(spec)); err != nil {
				return nil, cleanup, err
			}
			files = append(files, localPath)
			continue
		}

		dir, err := ensureTempDir()
		if err != nil {
			return nil, cleanup, err
		}

		filename := safeAttachmentFilename(spec, i)
		destination := uniqueDestination(dir, filename, i)
		switch {
		case spec.Data != "":
			if err := writeEncodedAttachment(destination, spec.Data, limitForAttachment(spec)); err != nil {
				return nil, cleanup, fmt.Errorf("materialize %s: %w", filename, err)
			}
		case strings.HasPrefix(strings.ToLower(spec.Source), "data:"):
			data, mimeType, err := decodeDataURL(spec.Source, limitForAttachment(spec))
			if err != nil {
				return nil, cleanup, fmt.Errorf("materialize %s: %w", filename, err)
			}
			if filepath.Ext(destination) == "" {
				if ext := extensionForMIME(mimeType); ext != "" {
					destination += ext
				}
			}
			if err := os.WriteFile(destination, data, 0o600); err != nil {
				return nil, cleanup, err
			}
		case isHTTPURL(spec.Source):
			if err := downloadAttachment(ctx, spec.Source, destination, limitForAttachment(spec)); err != nil {
				return nil, cleanup, fmt.Errorf("download %s: %w", filename, err)
			}
		case spec.FileID != "":
			return nil, cleanup, unresolvedFileIDError(spec.FileID)
		default:
			return nil, cleanup, fmt.Errorf("unsupported attachment source %q; use file_data, data URL, public HTTP(S) URL, or file:// path", spec.Source)
		}
		files = append(files, destination)
	}
	return files, cleanup, nil
}

func unresolvedFileIDError(fileID string) error {
	return fmt.Errorf("file_id %q cannot be resolved by ChatBang; send file_data, file_url, or an image URL/data URL instead", fileID)
}

func resolveLocalAttachment(source string) (string, bool, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", false, nil
	}
	if filepath.IsAbs(source) {
		return filepath.Clean(source), true, nil
	}
	if !strings.HasPrefix(strings.ToLower(source), "file://") {
		return "", false, nil
	}
	u, err := url.Parse(source)
	if err != nil {
		return "", false, fmt.Errorf("invalid file URL: %w", err)
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		return "", false, fmt.Errorf("file URL host must be empty or localhost")
	}
	path, err := url.PathUnescape(u.Path)
	if err != nil {
		return "", false, fmt.Errorf("invalid file URL path: %w", err)
	}
	return filepath.Clean(path), true, nil
}

func validateMaterializedFile(path string, limit int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("attachment %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("attachment %q is not a regular file", path)
	}
	if info.Size() > limit {
		return fmt.Errorf("attachment %q is %d bytes; limit is %d bytes", path, info.Size(), limit)
	}
	return nil
}

func limitForAttachment(spec attachmentSpec) int64 {
	if spec.Kind == "image" {
		return maxImageBytes
	}
	return maxAttachmentBytes
}

func writeEncodedAttachment(destination, encoded string, limit int64) error {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(encoded)), "data:") {
		data, _, err := decodeDataURL(encoded, limit)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	}

	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		default:
			return r
		}
	}, encoded)
	data, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return fmt.Errorf("file_data is not valid base64: %w", err)
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("decoded attachment is %d bytes; limit is %d bytes", len(data), limit)
	}
	return os.WriteFile(destination, data, 0o600)
}

func decodeDataURL(value string, limit int64) ([]byte, string, error) {
	value = strings.TrimSpace(value)
	comma := strings.IndexByte(value, ',')
	if !strings.HasPrefix(strings.ToLower(value), "data:") || comma < 0 {
		return nil, "", fmt.Errorf("invalid data URL")
	}
	meta := value[len("data:"):comma]
	payload := value[comma+1:]
	parts := strings.Split(meta, ";")
	mimeType := strings.TrimSpace(parts[0])
	isBase64 := false
	for _, part := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(part), "base64") {
			isBase64 = true
			break
		}
	}

	var data []byte
	var err error
	if isBase64 {
		cleaned := strings.Map(func(r rune) rune {
			switch r {
			case ' ', '\n', '\r', '\t':
				return -1
			default:
				return r
			}
		}, payload)
		data, err = base64.StdEncoding.DecodeString(cleaned)
	} else {
		var decoded string
		decoded, err = url.PathUnescape(payload)
		data = []byte(decoded)
	}
	if err != nil {
		return nil, mimeType, err
	}
	if int64(len(data)) > limit {
		return nil, mimeType, fmt.Errorf("decoded attachment is %d bytes; limit is %d bytes", len(data), limit)
	}
	return data, mimeType, nil
}

func safeAttachmentFilename(spec attachmentSpec, index int) string {
	name := strings.TrimSpace(spec.Filename)
	if name == "" {
		name = filenameFromSource(spec.Source)
	}
	if name == "" {
		ext := ""
		if spec.Kind == "image" {
			ext = ".png"
		}
		name = fmt.Sprintf("attachment-%d%s", index+1, ext)
	}
	name = filepath.Base(strings.ReplaceAll(name, "\x00", ""))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = fmt.Sprintf("attachment-%d", index+1)
	}
	if len(name) > 180 {
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		maxBase := 180 - len(ext)
		if maxBase < 1 {
			maxBase = 1
		}
		if len(base) > maxBase {
			base = base[:maxBase]
		}
		name = base + ext
	}
	return name
}

func filenameFromSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" || strings.HasPrefix(strings.ToLower(source), "data:") {
		return ""
	}
	if filepath.IsAbs(source) {
		return filepath.Base(source)
	}
	u, err := url.Parse(source)
	if err != nil {
		return ""
	}
	base := filepath.Base(u.Path)
	if base == "." || base == "/" {
		return ""
	}
	return base
}

func uniqueDestination(dir, name string, index int) string {
	destination := filepath.Join(dir, name)
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		return destination
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, index+1, ext))
}

func extensionForMIME(mimeType string) string {
	mimeType = strings.TrimSpace(strings.Split(mimeType, ";")[0])
	if mimeType == "" {
		return ""
	}
	exts, err := mime.ExtensionsByType(mimeType)
	if err != nil || len(exts) == 0 {
		return ""
	}
	return exts[0]
}

func isHTTPURL(source string) bool {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func downloadAttachment(ctx context.Context, source, destination string, limit int64) error {
	client := newAttachmentHTTPClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return fmt.Errorf("remote attachment is %d bytes; limit is %d bytes", resp.ContentLength, limit)
	}

	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, io.LimitReader(resp.Body, limit+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return closeErr
	}
	if n > limit {
		_ = os.Remove(destination)
		return fmt.Errorf("remote attachment exceeded %d-byte limit", limit)
	}
	return nil
}

func newAttachmentHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, candidate := range ips {
				if unsafeAttachmentIP(candidate.IP) {
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, fmt.Errorf("attachment URL host resolves only to private or unsafe addresses")
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

func unsafeAttachmentIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}
