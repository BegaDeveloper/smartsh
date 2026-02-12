package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"smartsh/internal/detector"
)

const (
	defaultOllamaURL   = "http://localhost:11434/api/generate"
	defaultOllamaModel = "llama3.1:8b"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	model      string
}

func NewClientFromEnv() *Client {
	baseURL := strings.TrimSpace(os.Getenv("SMARTSH_OLLAMA_URL"))
	if baseURL == "" {
		baseURL = defaultOllamaURL
	}

	model := strings.TrimSpace(os.Getenv("SMARTSH_MODEL"))
	if model == "" {
		model = defaultOllamaModel
	}

	return &Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		baseURL:    baseURL,
		model:      model,
	}
}

func (client *Client) GenerateIntent(ctx context.Context, userInput string, environment detector.Environment) (Response, error) {
	basePrompt := buildPrompt(userInput, environment)
	prompts := []string{
		basePrompt,
		basePrompt + "\n\nIMPORTANT: Return ONLY one raw JSON object. No markdown, no prose, no code fences.",
	}

	var lastError error
	for _, prompt := range prompts {
		payload := ollamaGenerateRequest{
			Model:  client.model,
			Prompt: prompt,
			Stream: false,
			Format: "json",
			Options: map[string]interface{}{
				"temperature": 0,
			},
		}

		requestBody, marshalError := json.Marshal(payload)
		if marshalError != nil {
			return Response{}, fmt.Errorf("marshal ollama request: %w", marshalError)
		}

		request, requestError := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL, bytes.NewReader(requestBody))
		if requestError != nil {
			return Response{}, fmt.Errorf("create ollama request: %w", requestError)
		}
		request.Header.Set("Content-Type", "application/json")

		response, responseError := client.httpClient.Do(request)
		if responseError != nil {
			return Response{}, fmt.Errorf("call ollama: %w", responseError)
		}
		body, readError := io.ReadAll(response.Body)
		response.Body.Close()
		if readError != nil {
			return Response{}, fmt.Errorf("read ollama response: %w", readError)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return Response{}, fmt.Errorf("ollama status %d: %s", response.StatusCode, string(body))
		}

		var ollamaResponse ollamaGenerateResponse
		if unmarshalError := json.Unmarshal(body, &ollamaResponse); unmarshalError != nil {
			return Response{}, fmt.Errorf("decode ollama payload: %w", unmarshalError)
		}

		intentResponse, parseError := parseStrictResponseJSON(ollamaResponse.Response)
		if parseError == nil {
			return intentResponse, nil
		}
		lastError = &StrictJSONResponseError{
			Cause:       parseError,
			RawResponse: ollamaResponse.Response,
		}
	}

	if lastError != nil {
		return Response{}, lastError
	}
	return Response{}, fmt.Errorf("model did not return a valid response")
}

func buildPrompt(userInput string, environment detector.Environment) string {
	environmentJSON, _ := json.Marshal(environment)

	return fmt.Sprintf(`You are smartsh command planner.
Return only strict JSON object with this exact schema and nothing else:
{"intent": string, "command": string, "confidence": number, "risk": "low | medium | high"}

Rules:
- command must be executable in %s
- keep command minimal and practical
- risk must reflect command danger
- no markdown, no prose, no code fences

Environment:
%s

User request:
%s`, environment.OS, string(environmentJSON), userInput)
}

func parseStrictResponseJSON(raw string) (Response, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Response{}, fmt.Errorf("model returned empty response")
	}
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return Response{}, fmt.Errorf("model response is not strict JSON object")
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()

	var intentResponse Response
	if decodeError := decoder.Decode(&intentResponse); decodeError != nil {
		return Response{}, fmt.Errorf("invalid JSON schema from model: %w", decodeError)
	}

	var trailing json.RawMessage
	if decodeError := decoder.Decode(&trailing); decodeError != io.EOF {
		return Response{}, fmt.Errorf("model returned non-JSON trailing content")
	}

	intentResponse.Intent = strings.TrimSpace(intentResponse.Intent)
	intentResponse.Command = strings.TrimSpace(intentResponse.Command)
	intentResponse.Risk = normalizeRisk(intentResponse.Risk)

	if intentResponse.Intent == "" {
		return Response{}, fmt.Errorf("model returned empty intent")
	}
	if intentResponse.Command == "" {
		return Response{}, fmt.Errorf("model returned empty command")
	}
	if intentResponse.Confidence < 0 || intentResponse.Confidence > 1 {
		return Response{}, fmt.Errorf("model returned confidence out of range [0,1]")
	}
	if intentResponse.Risk == "" {
		return Response{}, fmt.Errorf("model returned invalid risk")
	}

	return intentResponse, nil
}

func normalizeRisk(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	default:
		return ""
	}
}

func FormatConfidence(value float64) string {
	return fmt.Sprintf("%.2f", value)
}
