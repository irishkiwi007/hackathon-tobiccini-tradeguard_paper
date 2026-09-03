// Package llm wraps Fireworks AI's OpenAI-compatible chat completion
// endpoint. This is the reasoning step: given account context, a quote,
// and news, it returns a structured trade proposal (or a decision not
// to propose anything — "no trade" is always a valid, and often
// correct, output).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

func New(apiKey, model, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		http:    &http.Client{},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	// Ask for a compact JSON object back so the agent can parse it
	// directly into a models.ProposedTrade. Adjust the schema described
	// in the system prompt (see internal/agent) if you add fields.
	ResponseFormat map[string]string `json:"response_format,omitempty"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
	// VERIFIED against Fireworks' own API reference
	// (docs.fireworks.ai/api-reference/post-chatcompletions), not a
	// third-party guess: "none" disables reasoning computation entirely
	// on models that support it (Kimi, DeepSeek, GLM, Qwen3, etc.).
	//
	// This matters more than it looks like it should. Reasoning-capable
	// models (which is what you get if FIREWORKS_MODEL points at a Kimi
	// model, as this project's setup did during development) count
	// their reasoning tokens against the SAME max_tokens budget as the
	// actual answer — Fireworks' own Kimi docs confirm reasoning_content
	// + content together must fit under max_tokens. Without disabling
	// reasoning, a verbose reasoning trace can crowd out or truncate the
	// JSON this code needs to parse, and json.Unmarshal in
	// internal/agent will fail unpredictably depending on how much
	// budget reasoning happened to consume that call. Ignored harmlessly
	// on models that don't support reasoning at all.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Propose sends the system + context prompt and returns the raw JSON
// text the model produced. internal/agent is responsible for parsing
// and validating it before anything touches the store.
func (c *Client) Propose(ctx context.Context, systemPrompt, userContext string) (string, error) {
	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContext},
		},
		ResponseFormat: map[string]string{"type": "json_object"},
		// 512 was tight enough that a verbose "reasoning" field (seen in
		// practice once the prompt started asking the model to reason
		// about a position-size cap) could run past it and get the
		// response truncated mid-string, breaking json.Unmarshal in
		// internal/agent with "unexpected end of JSON input". The prompt
		// itself is the real fix (see agent.go's instruction to keep
		// "reasoning" a conclusion, not a scratchpad) — this bump is
		// just headroom on top of that, not a substitute for it.
		MaxTokens:       900,
		ReasoningEffort: "none",
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call fireworks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fireworks returned status %d", resp.StatusCode)
	}

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("fireworks returned no choices")
	}

	return parsed.Choices[0].Message.Content, nil
}
