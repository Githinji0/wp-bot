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
	groqEndpoint = "https://api.groq.com/openai/v1/chat/completions"
	groqTimeout  = 30 * time.Second
)

// systemPrompt sets the assistant's persona and reply style.
const systemPrompt = `You are a helpful, friendly WhatsApp assistant. 
Keep replies concise and conversational — this is a chat app, not an essay.
Use WhatsApp formatting: *bold*, _italic_, bullet points with dashes.`

// --- Groq REST API request/response structures ---

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqRequest struct {
	Model    string        `json:"model"`
	Messages []groqMessage `json:"messages"`
}

type groqResponse struct {
	Choices []struct {
		Message groqMessage `json:"message"`
	} `json:"choices"`
	Error *groqError `json:"error,omitempty"`
}

type groqError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

// askGemini queries the Groq Llama 3.3 model using direct REST API calls.
// We keep the function name to avoid breaking the event handler in handler.go.
func askGemini(userMessage string) (string, error) {
	if Config.GroqKey == "" {
		return "", fmt.Errorf("Groq API key is not configured in .env")
	}

	reqBody := groqRequest{
		Model: "llama-3.3-70b-versatile",
		Messages: []groqMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", groqEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+Config.GroqKey)

	httpClient := &http.Client{Timeout: groqTimeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var groqErrResp groqResponse
		if err := json.Unmarshal(raw, &groqErrResp); err == nil && groqErrResp.Error != nil {
			return "", fmt.Errorf("Groq API error (%s): %s", groqErrResp.Error.Code, groqErrResp.Error.Message)
		}
		return "", fmt.Errorf("Groq API returned status %d: %s", resp.StatusCode, string(raw))
	}

	var groqResp groqResponse
	if err := json.Unmarshal(raw, &groqResp); err != nil {
		return "", fmt.Errorf("failed to parse response JSON: %w", err)
	}

	if len(groqResp.Choices) == 0 {
		return "🤔 Empty response from AI. Please try again.", nil
	}

	reply := strings.TrimSpace(groqResp.Choices[0].Message.Content)
	if reply == "" {
		return "🤔 Empty response from AI. Please try again.", nil
	}

	return reply, nil
}
