package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	geminiEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"
	geminiTimeout  = 30 * time.Second
)

// modelFallbackChain lists models to try in order if the previous one fails.
// Only include models confirmed available on the v1beta generateContent endpoint.
var modelFallbackChain = []string{
	"gemini-2.0-flash",
	"gemini-2.0-flash-lite",
}

// systemPrompt sets the assistant's persona and reply style.
const systemPrompt = `You are a helpful, friendly WhatsApp assistant. 
Keep replies concise and conversational — this is a chat app, not an essay.
Use WhatsApp formatting: *bold*, _italic_, bullet points with dashes.`

// --- Gemini REST API request/response structures ---

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"system_instruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	Error *geminiError `json:"error,omitempty"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// isQuotaError returns true if this is a rate-limit / quota-exceeded error.
func (e *geminiError) isQuotaError() bool {
	return e.Code == 429 ||
		strings.Contains(e.Message, "quota") ||
		strings.Contains(e.Message, "RESOURCE_EXHAUSTED") ||
		e.Status == "RESOURCE_EXHAUSTED"
}

// isZeroQuota returns true when the project has limit:0 — meaning the API key
// has NO quota at all (wrong key, wrong project, or billing not enabled).
// In this case retrying other models is pointless — all share the same quota.
func (e *geminiError) isZeroQuota() bool {
	return e.isQuotaError() && strings.Contains(e.Message, "limit: 0")
}

// askGemini sends a message to the Gemini REST API, trying each model in
// the fallback chain until one succeeds or all are exhausted.
func askGemini(userMessage string) (string, error) {
	if Config.GeminiKey == "" || Config.GeminiKey == "your_gemini_api_key_here" {
		return "", fmt.Errorf("Gemini API key is not configured in .env")
	}

	reqBody := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: userMessage}}},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	httpClient := &http.Client{Timeout: geminiTimeout}

	// Try each model in the fallback chain
	for _, model := range modelFallbackChain {
		reply, apiErr, err := callGemini(httpClient, model, bodyBytes)
		if err != nil {
			fmt.Printf("⚠️  Gemini [%s] network error: %v\n", model, err)
			continue
		}
		if apiErr != nil {
			if apiErr.isZeroQuota() {
				// limit:0 means the whole project/key has no quota — stop immediately
				fmt.Printf("❌ Gemini API key has zero quota (limit: 0). A new key is needed.\n")
				return "", fmt.Errorf("Gemini API key has zero quota (limit: 0)")
			}
			if apiErr.isQuotaError() {
				fmt.Printf("⚠️  Gemini [%s] quota exceeded, trying next model...\n", model)
				continue
			}
			return "", fmt.Errorf("Gemini API error: %s", apiErr.Message)
		}
		return reply, nil
	}

	// All models exhausted
	return "", fmt.Errorf("all models exhausted (quota exceeded or rate limited)")
}

// callGemini makes a single REST call to one Gemini model.
// Returns (reply, apiError, networkError). Only one of apiError/networkError will be non-nil.
func callGemini(client *http.Client, model string, bodyBytes []byte) (string, *geminiError, error) {
	url := fmt.Sprintf(geminiEndpoint, model) + "?key=" + Config.GeminiKey

	resp, err := client.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(raw, &gemResp); err != nil {
		return "", nil, fmt.Errorf("failed to parse response JSON: %w", err)
	}

	if gemResp.Error != nil {
		return "", gemResp.Error, nil
	}

	if len(gemResp.Candidates) == 0 || len(gemResp.Candidates[0].Content.Parts) == 0 {
		return "🤔 Empty response from AI. Please try again.", nil, nil
	}

	reply := strings.TrimSpace(gemResp.Candidates[0].Content.Parts[0].Text)
	if reply == "" {
		return "🤔 Empty response from AI. Please try again.", nil, nil
	}
	return reply, nil, nil
}
