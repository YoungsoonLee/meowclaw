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

// DefaultOpenAIBaseURL is the official API host; path comes from chatPath or DefaultOpenAIChatPath.
const DefaultOpenAIBaseURL = "https://api.openai.com"

type OpenAI struct {
	apiKey   string
	model    string
	baseURL  string
	chatPath string
	client   *http.Client
}

// NewOpenAI creates a client. baseURL empty → DefaultOpenAIBaseURL; chatPath empty → DefaultOpenAIChatPath.
func NewOpenAI(apiKey, model, baseURL, chatPath string) *OpenAI {
	if model == "" {
		model = "gpt-4o"
	}
	if baseURL == "" {
		baseURL = DefaultOpenAIBaseURL
	}
	return &OpenAI{
		apiKey:   apiKey,
		model:    model,
		baseURL:  strings.TrimRight(baseURL, "/"),
		chatPath: chatPath,
		client:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (o *OpenAI) chatCompletionsEndpoint() string {
	path := o.chatPath
	if path == "" {
		path = DefaultOpenAIChatPath
	}
	return joinBaseAndPath(o.baseURL, path)
}

func (o *OpenAI) Name() string { return "openai" }

func (o *OpenAI) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := o.buildRequestBody(req)

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.chatCompletionsEndpoint(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result openAIResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("openai returned no choices")
	}

	return &ChatResponse{
		Content:      result.Choices[0].Message.Content,
		Model:        result.Model,
		InputTokens:  result.Usage.PromptTokens,
		OutputTokens: result.Usage.CompletionTokens,
		FinishReason: result.Choices[0].FinishReason,
	}, nil
}

func (o *OpenAI) ChatStream(ctx context.Context, req *ChatRequest, onChunk StreamCallback) (*ChatResponse, error) {
	body := o.buildRequestBody(req)
	body["stream"] = true
	body["stream_options"] = map[string]interface{}{"include_usage": true}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.chatCompletionsEndpoint(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai stream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai error (status %d): %s", resp.StatusCode, string(respBody))
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
		if data == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		chatResp.Model = chunk.Model

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta.Content
			if delta != "" {
				fullContent.WriteString(delta)
				onChunk(delta)
			}
			if chunk.Choices[0].FinishReason != "" {
				chatResp.FinishReason = chunk.Choices[0].FinishReason
			}
		}

		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			chatResp.InputTokens = chunk.Usage.PromptTokens
			chatResp.OutputTokens = chunk.Usage.CompletionTokens
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("openai stream read: %w", err)
	}

	chatResp.Content = fullContent.String()
	return chatResp, nil
}

func (o *OpenAI) buildRequestBody(req *ChatRequest) map[string]interface{} {
	model := req.Model
	if model == "" {
		model = o.model
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	body := map[string]interface{}{
		"model":      model,
		"messages":   req.Messages,
		"max_tokens": maxTokens,
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	return body
}

type openAIResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openAIStreamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}
