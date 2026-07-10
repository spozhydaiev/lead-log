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
	GenerateTicket(ctx context.Context, input string) (models.TicketDraft, error)
	SummarizeDaily(ctx context.Context, input string) (string, error)
	SummarizeWeekly(ctx context.Context, input string) (string, error)
	Model() string
	SummarizePerson(ctx context.Context, input string) (string, error)
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

func (c *Client) SummarizePerson(ctx context.Context, input string) (string, error) {
	prompt := personPrompt() + "\n\nPerson context:\n" + input
	return c.chatText(ctx, prompt)
}

func personPrompt() string {
	return `You are preparing a concise person context summary for a team lead.
Respond in Ukrainian.
Do not evaluate the employee. Do not score people. Do not recommend HR decisions.
Only summarize manager-provided notes.
Group information into:
1. Open follow-ups
2. Positive signals
3. Concerns or risks
4. Growth topics
5. Suggested 1:1 topics
Every claim must be based on the provided notes.`
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

func (c *Client) GenerateTicket(ctx context.Context, input string) (models.TicketDraft, error) {
	prompt := ticketPrompt() + "\n\nInput:\n" + input
	content, err := c.chatJSON(ctx, prompt)
	if err != nil {
		return models.TicketDraft{}, err
	}
	var draft models.TicketDraft
	if err := json.Unmarshal([]byte(content), &draft); err != nil {
		return models.TicketDraft{}, fmt.Errorf("parse ticket json: %w; content=%s", err, content)
	}
	return draft, nil
}

func (c *Client) SummarizeDaily(ctx context.Context, input string) (string, error) {
	prompt := dailyPrompt() + "\n\nSource notes/actions:\n" + input
	return c.chatText(ctx, prompt)
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
All user-facing summaries, actions, ticket drafts, agendas and explanations must be in Ukrainian, regardless of the input language.
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
      "type": "positive_signal|concern|growth_topic|context|follow_up_needed|commitment|decision|risk|blocker|review_evidence",
      "theme": "ownership|communication|delivery|collaboration|technical_quality|reliability|mentorship|process|",
      "text": "neutral source-bound note",
      "include_in_review": true
    }
  ],
  "ticket_drafts": [
    {"title": "", "context": "", "problem": "", "acceptance_criteria": [""]}
  ],
  "suggested_questions": ["clarifying questions if useful"]
}`
}

func ticketPrompt() string {
	return `Create a concise Jira-style ticket draft from the manager-provided input.
Do not invent technical details. If context is missing, write conservative acceptance criteria.
Language rules:
- Respond in English.
- Keep all user-facing text in English.

Return valid JSON only:
{
  "title": "",
  "context": "",
  "problem": "",
  "acceptance_criteria": [""]
}`
}

func dailyPrompt() string {
	return `You are preparing a daily manager digest from manager-provided notes created today.
Do not evaluate employees. Do not score people. Do not recommend HR decisions.
Do not invent facts. Every claim must be based on the provided notes.
Language rules:
- Respond in Ukrainian.
- Keep all user-facing text in Ukrainian.
- Do not translate or transliterate person names freely.
- Use canonical display names from Known people when a match exists.

Entity rules:
- People are identity entities, not just strings.
- Match names against Known people and aliases.
- If a mentioned name likely refers to a known person, return that person's person_id and canonical display_name.
- If unsure, return needs_confirmation=true.
- Never create a new person only because the name appears in a different language or transliteration.

Focus on:
1. Today summary
2. Open loops and follow-ups
3. Ticket candidates
4. People highlights, grouped neutrally as positive signals, concerns, risks, blockers, growth topics, commitments, decisions, or context
5. Suggested 1:1 topics
6. Questions or unclear items that need confirmation
Keep it concise, practical, and source-bound. Include note numbers when useful.
`
}

func weeklyPrompt() string {
	return `You are preparing a weekly manager digest from manager-provided notes and actions.
Do not evaluate employees. Do not score people. Do not recommend HR decisions.
Summarize open loops, people highlights, risks, positive signals, concerns, and suggested 1:1 topics.
Every claim must be phrased as based on the provided notes.
Keep it concise and practical.
Always respond in Ukrainian.
All user-facing summaries, actions, ticket drafts, agendas and explanations must be in Ukrainian, regardless of the input language.
Do not switch language unless the user explicitly asks.
Respond in Ukrainian, but preserve person names as they are stored in the user's canonical people database.
Do not translate or transliterate person names unless mapping to an existing canonical person.`
}
