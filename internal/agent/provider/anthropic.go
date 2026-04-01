package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultAnthropicBaseURL is the official host; path comes from messagesPath or DefaultAnthropicMessagesPath.
const DefaultAnthropicBaseURL = "https://api.anthropic.com"

type Anthropic struct {
	apiKey       string
	model        string
	baseURL      string
	messagesPath string
	client       *http.Client
}

// NewAnthropic creates a client. baseURL empty → DefaultAnthropicBaseURL; messagesPath empty → DefaultAnthropicMessagesPath.
func NewAnthropic(apiKey, model, baseURL, messagesPath string) *Anthropic {
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	if baseURL == "" {
		baseURL = DefaultAnthropicBaseURL
	}
	return &Anthropic{
		apiKey:       apiKey,
		model:        model,
		baseURL:      strings.TrimRight(baseURL, "/"),
		messagesPath: messagesPath,
		client:       &http.Client{Timeout: 120 * time.Second},
	}
}

func (a *Anthropic) messagesEndpoint() string {
	path := a.messagesPath
	if path == "" {
		path = DefaultAnthropicMessagesPath
	}
	return joinBaseAndPath(a.baseURL, path)
}

func (a *Anthropic) Name() string { return "anthropic" }

func (a *Anthropic) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := a.buildRequestBody(req)

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.messagesEndpoint(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	a.setHeaders(httpReq)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result anthropicResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	content := ""
	for _, block := range result.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &ChatResponse{
		Content:      content,
		Model:        result.Model,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		FinishReason: result.StopReason,
	}, nil
}

func (a *Anthropic) ChatStream(ctx context.Context, req *ChatRequest, onChunk StreamCallback) (*ChatResponse, error) {
	body := a.buildRequestBody(req)
	body["stream"] = true

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.messagesEndpoint(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	a.setHeaders(httpReq)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic stream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var fullContent strings.Builder
	chatResp := &ChatResponse{}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "message_start":
			if event.Message != nil {
				chatResp.Model = event.Message.Model
				chatResp.InputTokens = event.Message.Usage.InputTokens
			}

		case "content_block_delta":
			if event.Delta != nil && event.Delta.Type == "text_delta" {
				fullContent.WriteString(event.Delta.Text)
				onChunk(event.Delta.Text)
			}

		case "message_delta":
			if event.Delta != nil {
				chatResp.FinishReason = event.Delta.StopReason
			}
			if event.Usage != nil {
				chatResp.OutputTokens = event.Usage.OutputTokens
			}

		case "message_stop":
			// stream complete
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("anthropic stream read: %w", err)
	}

	chatResp.Content = fullContent.String()
	return chatResp, nil
}

func (a *Anthropic) buildRequestBody(req *ChatRequest) map[string]interface{} {
	model := req.Model
	if model == "" {
		model = a.model
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	var systemMsg string
	var messages []map[string]string
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			systemMsg = m.Content
			continue
		}
		messages = append(messages, map[string]string{
			"role":    string(m.Role),
			"content": m.Content,
		})
	}

	body := map[string]interface{}{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   messages,
	}
	if systemMsg != "" {
		body["system"] = systemMsg
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	return body
}

func (a *Anthropic) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}

type anthropicResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	StopReason string `json:"stop_reason"`
}

type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Message *struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	} `json:"message,omitempty"`
	Delta *struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta,omitempty"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
}
