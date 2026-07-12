package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spozhydaiev/lead-log/internal/models"
)

type ClientLLM interface {
	ParseManagerNote(ctx context.Context, raw string) (models.ParsedNote, error)
	ProcessDaily(ctx context.Context, input string) (models.DailyDigest, error)
	SummarizeWeekly(ctx context.Context, input string) (string, error)
	Model() string
}

type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: 45 * time.Second},
	}
}

type chatCompletionRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat responseFormat `json:"response_format"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func (c *Client) Model() string {
	return c.model
}

func (c *Client) ParseManagerNote(ctx context.Context, raw string) (models.ParsedNote, error) {
	prompt := systemPrompt() + "\n\nManager note:\n" + raw
	content, err := c.chatJSON(ctx, prompt)
	if err != nil {
		return models.ParsedNote{}, err
	}

	var parsed models.ParsedNote
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return models.ParsedNote{}, fmt.Errorf("parse llm json: %w; content=%s", err, content)
	}
	return parsed, nil
}

func (c *Client) ProcessDaily(ctx context.Context, input string) (models.DailyDigest, error) {
	prompt := dailyPrompt() + "\n\nSource notes/actions:\n" + input
	content, err := c.chatJSON(ctx, prompt)
	if err != nil {
		return models.DailyDigest{}, err
	}
	return ParseDailyDigestJSON(content)
}

func (c *Client) SummarizeWeekly(ctx context.Context, input string) (string, error) {
	prompt := weeklyPrompt() + "\n\nSource notes/actions:\n" + input
	return c.chatText(ctx, prompt)
}

func (c *Client) chatJSON(ctx context.Context, prompt string) (string, error) {
	return c.chat(ctx, prompt, responseFormat{Type: "json_object"})
}

func (c *Client) chatText(ctx context.Context, prompt string) (string, error) {
	return c.chat(ctx, prompt, responseFormat{Type: "text"})
}

func (c *Client) chat(ctx context.Context, prompt string, format responseFormat) (string, error) {
	reqBody := chatCompletionRequest{
		Model:          c.model,
		Temperature:    0.1,
		ResponseFormat: format,
		Messages:       []chatMessage{{Role: "user", Content: prompt}},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm error status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var out chatCompletionResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", errors.New("llm returned no choices")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func systemPrompt() string {
	return `You are a private assistant for a team lead.
Your job is to structure only manager-provided notes.
Do not evaluate employees.
Do not score people.
Do not recommend HR decisions like promote, fire, PIP, compensation, disciplinary action.
Do not invent facts.
If something is uncertain, keep it as a suggested follow-up or question.
Use source-bound, careful wording.
Always respond in Ukrainian.
All user-facing summaries, actions and explanations must be in Ukrainian, regardless of the input language.
Do not switch language unless the user explicitly asks.
Respond in Ukrainian, but preserve person names as they are stored in the user's canonical people database.
Do not translate or transliterate person names unless mapping to an existing canonical person.

Return valid JSON only with this shape:
{
  "summary": "short neutral summary",
  "tags": ["short_tags"],
  "actions": [
    {"title": "action title", "linked_person_name": "optional", "output_type": "ticket|meeting|message|reminder|"}
  ],
  "people_notes": [
    {
      "person_name": "name",
      "type": "positive_signal|concern|growth_topic|context|follow_up_needed|commitment|decision|risk|blocker",
      "theme": "ownership|communication|delivery|collaboration|technical_quality|reliability|mentorship|process|",
      "text": "neutral source-bound note",
      "include_in_review": true
    }
  ],
  "suggested_questions": ["clarifying questions if useful"]
}`
}

func dailyPrompt() string {
	return `You are preparing a daily manager digest from manager-provided notes created today.
Do not evaluate employees. Do not score people. Do not recommend HR decisions.
Do not invent facts. Every claim must be based on the provided notes.
Language rules:
- Respond in Ukrainian for all user-facing field values.
- Keep text concise, neutral, practical, and source-bound.
- Do not translate or transliterate person names freely.
- Use canonical display names from Known people when a match exists.

Return valid JSON only with this exact shape. Use empty arrays for empty sections and null for missing owner/due_hint:
{
  "short_summary": "short neutral Ukrainian summary",
  "open_loops": [
    {"title": "action or follow-up", "owner": "person or null", "due_hint": "date/time hint or null", "source_note_ids": [1]}
  ],
  "ticket_candidates": [
    {"title": "ticket title", "context": "source-backed context", "owner": "person or null", "source_note_ids": [1]}
  ],
  "people_highlights": [
    {
      "person_name": "name",
      "type": "positive_signal|concern|follow_up_needed|growth_topic|context|commitment|risk",
      "theme": "communication|ownership|delivery|collaboration|technical_quality|reliability|hiring|release|process|other",
      "text": "neutral source-backed note",
      "source_note_ids": [1]
    }
  ],
  "decisions": [
    {"text": "decision or agreement", "source_note_ids": [1]}
  ],
  "suggested_next_steps": [
    {"text": "suggested next step", "source_note_ids": [1]}
  ],
  "unclear_items": [
    {"text": "unclear item or question", "source_note_ids": [1]}
  ]
}`
}
func weeklyPrompt() string {
	return `You are preparing a weekly manager digest from manager-provided notes and actions.
Do not evaluate employees. Do not score people. Do not recommend HR decisions.
Summarize what happened, important topics, open loops, risks, decisions, suggested next steps, and what the manager worked on personally.
Every claim must be phrased as based on the provided notes.
Keep it concise and practical.
Always respond in Ukrainian.
All user-facing summaries, actions and explanations must be in Ukrainian, regardless of the input language.
Do not switch language unless the user explicitly asks.
Respond in Ukrainian, but preserve person names as they are stored in the user's canonical people database.
Do not translate or transliterate person names unless mapping to an existing canonical person.`
}
