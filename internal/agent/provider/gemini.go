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

const DefaultGeminiBaseURL = "https://generativelanguage.googleapis.com"

// DefaultGeminiGeneratePath uses :generateContent; the model name is interpolated at call time.
const DefaultGeminiGeneratePath = "/v1beta/models/%s:generateContent"

// DefaultGeminiStreamPath uses :streamGenerateContent with SSE alt.
const DefaultGeminiStreamPath = "/v1beta/models/%s:streamGenerateContent?alt=sse"

type Gemini struct {
	apiKey       string
	model        string
	baseURL      string
	generatePath string
	client       *http.Client
}

// NewGemini creates a Gemini API client.
// baseURL empty → DefaultGeminiBaseURL; generatePath empty → DefaultGeminiGeneratePath.
func NewGemini(apiKey, model, baseURL, generatePath string) *Gemini {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	if baseURL == "" {
		baseURL = DefaultGeminiBaseURL
	}
	return &Gemini{
		apiKey:       apiKey,
		model:        model,
		baseURL:      strings.TrimRight(baseURL, "/"),
		generatePath: generatePath,
		client:       &http.Client{Timeout: 120 * time.Second},
	}
}

func (g *Gemini) generateEndpoint(model string) string {
	path := g.generatePath
	if path == "" {
		path = DefaultGeminiGeneratePath
	}
	return joinBaseAndPath(g.baseURL, fmt.Sprintf(path, model))
}

func (g *Gemini) streamEndpoint(model string) string {
	path := DefaultGeminiStreamPath
	return joinBaseAndPath(g.baseURL, fmt.Sprintf(path, model))
}

func (g *Gemini) Name() string { return "gemini" }

func (g *Gemini) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = g.model
	}
	body := g.buildRequestBody(req)

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := g.generateEndpoint(model) + "?key=" + g.apiKey
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result geminiResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	content := extractGeminiText(result.Candidates)
	finishReason := ""
	if len(result.Candidates) > 0 {
		finishReason = result.Candidates[0].FinishReason
	}

	return &ChatResponse{
		Content:      content,
		Model:        model,
		InputTokens:  result.UsageMetadata.PromptTokenCount,
		OutputTokens: result.UsageMetadata.CandidatesTokenCount,
		FinishReason: finishReason,
	}, nil
}

func (g *Gemini) ChatStream(ctx context.Context, req *ChatRequest, onChunk StreamCallback) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = g.model
	}
	body := g.buildRequestBody(req)

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := g.streamEndpoint(model) + "&key=" + g.apiKey
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini stream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var fullContent strings.Builder
	chatResp := &ChatResponse{Model: model}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var chunk geminiResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		delta := extractGeminiText(chunk.Candidates)
		if delta != "" {
			fullContent.WriteString(delta)
			onChunk(delta)
		}

		if len(chunk.Candidates) > 0 && chunk.Candidates[0].FinishReason != "" {
			chatResp.FinishReason = chunk.Candidates[0].FinishReason
		}

		if chunk.UsageMetadata.PromptTokenCount > 0 {
			chatResp.InputTokens = chunk.UsageMetadata.PromptTokenCount
		}
		if chunk.UsageMetadata.CandidatesTokenCount > 0 {
			chatResp.OutputTokens = chunk.UsageMetadata.CandidatesTokenCount
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("gemini stream read: %w", err)
	}

	chatResp.Content = fullContent.String()
	return chatResp, nil
}

func (g *Gemini) buildRequestBody(req *ChatRequest) map[string]interface{} {
	// Convert ChatMessages to Gemini's contents format.
	// Gemini uses "user" and "model" roles; system instructions go in a separate field.
	var systemInstruction string
	var contents []geminiContent

	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem:
			systemInstruction = m.Content
		case RoleUser:
			contents = append(contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{Text: m.Content}},
			})
		case RoleAssistant:
			contents = append(contents, geminiContent{
				Role:  "model",
				Parts: []geminiPart{{Text: m.Content}},
			})
		}
	}

	body := map[string]interface{}{
		"contents": contents,
	}

	if systemInstruction != "" {
		body["systemInstruction"] = geminiContent{
			Parts: []geminiPart{{Text: systemInstruction}},
		}
	}

	// Generation config
	genConfig := map[string]interface{}{}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	genConfig["maxOutputTokens"] = maxTokens
	if req.Temperature > 0 {
		genConfig["temperature"] = req.Temperature
	}
	body["generationConfig"] = genConfig

	return body
}

func extractGeminiText(candidates []geminiCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, part := range candidates[0].Content.Parts {
		sb.WriteString(part.Text)
	}
	return sb.String()
}

// --- Gemini API types ---

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate  `json:"candidates"`
	UsageMetadata geminiUsageMetadata `json:"usageMetadata"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}
