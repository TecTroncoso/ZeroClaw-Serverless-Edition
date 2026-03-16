// Package channels implements communication channels for ZeroClaw.
// This module provides Slack bot integration via Events API Webhook.
package channels

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// ============================================================================
// SLACK CHANNEL
// ============================================================================

// SlackChannel implements the Channel interface for Slack.
type SlackChannel struct {
	botToken   string
	httpClient *http.Client
}

// NewSlackChannel creates a new Slack channel.
func NewSlackChannel() *SlackChannel {
	return NewSlackChannelWithToken(os.Getenv("SLACK_BOT_TOKEN"))
}

// NewSlackChannelWithToken creates a Slack channel with explicit token.
func NewSlackChannelWithToken(token string) *SlackChannel {
	return &SlackChannel{
		botToken: token,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
	}
}

// Name returns the channel name.
func (c *SlackChannel) Name() string {
	return "slack"
}

// Send sends a message to a Slack channel.
// recipient should be the channel ID (e.g., "C1234567890").
func (c *SlackChannel) Send(recipient, message string) error {
	if c.botToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN not configured")
	}

	url := "https://slack.com/api/chat.postMessage"

	payload := map[string]interface{}{
		"channel": recipient,
		"text":    message,
		"mrkdwn":  true,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// Check for Slack API error
	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &slackResp); err == nil {
		if !slackResp.OK {
			return fmt.Errorf("slack API error: %s", slackResp.Error)
		}
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SendBlocks sends a message with Block Kit blocks.
func (c *SlackChannel) SendBlocks(recipient string, text string, blocks []SlackBlock) error {
	if c.botToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN not configured")
	}

	url := "https://slack.com/api/chat.postMessage"

	payload := map[string]interface{}{
		"channel": recipient,
		"text":    text, // Fallback text
		"blocks":  blocks,
		"mrkdwn":  true,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send blocks: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &slackResp); err == nil {
		if !slackResp.OK {
			return fmt.Errorf("slack API error: %s", slackResp.Error)
		}
	}

	return nil
}

// SendEphemeral sends an ephemeral message (only visible to one user).
func (c *SlackChannel) SendEphemeral(channel, user, message string) error {
	if c.botToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN not configured")
	}

	url := "https://slack.com/api/chat.postEphemeral"

	payload := map[string]interface{}{
		"channel": channel,
		"user":    user,
		"text":    message,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send ephemeral: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

// SendTyping sends a typing indicator to a Slack channel.
// Note: Slack does not have a formal API for typing indicators yet, it uses RTM which is deprecated.
// Here we'll just mock it and do nothing to satisfy the interface.
func (c *SlackChannel) SendTyping(ctx context.Context, recipient string) error {
	// Not natively supported by Slack Web API (needs WebSocket/RTM or specific app configs)
	return nil
}

// ============================================================================
// SLACK BLOCK KIT TYPES
// ============================================================================

// SlackBlock represents a Slack Block Kit block.
type SlackBlock struct {
	Type      string       `json:"type"`
	Text      *SlackText   `json:"text,omitempty"`
	Fields    []SlackText  `json:"fields,omitempty"`
	Elements  []SlackElement `json:"elements,omitempty"`
	Accessory *SlackElement `json:"accessory,omitempty"`
}

// SlackText represents a text object in Block Kit.
type SlackText struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

// SlackElement represents an element in Block Kit.
type SlackElement struct {
	Type     string     `json:"type"`
	Text     *SlackText `json:"text,omitempty"`
	ActionID string     `json:"action_id,omitempty"`
	URL      string     `json:"url,omitempty"`
	Value    string     `json:"value,omitempty"`
}

// NewSectionBlock creates a section block.
func NewSectionBlock(text string) SlackBlock {
	return SlackBlock{
		Type: "section",
		Text: &SlackText{
			Type: "mrkdwn",
			Text: text,
		},
	}
}

// NewDividerBlock creates a divider block.
func NewDividerBlock() SlackBlock {
	return SlackBlock{
		Type: "divider",
	}
}

// NewContextBlock creates a context block.
func NewContextBlock(text string) SlackBlock {
	return SlackBlock{
		Type: "context",
		Elements: []SlackElement{
			{
				Type: "mrkdwn",
				Text: &SlackText{
					Type: "mrkdwn",
					Text: text,
				},
			},
		},
	}
}

// ============================================================================
// SLACK EVENTS API PARSING
// ============================================================================

// SlackEvent represents an incoming Slack event.
type SlackEvent struct {
	Token       string          `json:"token"`
	TeamID      string          `json:"team_id"`
	APIAppID    string          `json:"api_app_id"`
	Type        string          `json:"type"`
	Event       *SlackEventBody `json:"event"`
	EventID     string          `json:"event_id"`
	EventTime   int64           `json:"event_time"`
	Challenge   string          `json:"challenge"`
}

// SlackEventBody contains the actual event data.
type SlackEventBody struct {
	Type        string      `json:"type"`
	User        string      `json:"user"`
	Text        string      `json:"text"`
	Channel     string      `json:"channel"`
	ChannelType string      `json:"channel_type"`
	Ts          string      `json:"ts"`
	EventTs     string      `json:"event_ts"`
	ClientMsgID string      `json:"client_msg_id"`
	SubType     string      `json:"subtype"`
	BotID       string      `json:"bot_id"`
	ThreadTs    string      `json:"thread_ts"`
	Files       []SlackFile `json:"files"`
}

// SlackFile represents a file attachment.
type SlackFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Mimetype    string `json:"mimetype"`
	URLPrivate  string `json:"url_private"`
}

// Event types
const (
	SlackEventTypeURLVerification = "url_verification"
	SlackEventTypeEventCallback   = "event_callback"
)

// Message event types
const (
	SlackEventMessage        = "message"
	SlackEventAppMention     = "app_mention"
	SlackEventMessageChanged = "message_changed"
	SlackEventMessageDeleted = "message_deleted"
	SlackEventBotMessage     = "bot_message"
)

// ParseSlackEvent parses a Slack event request.
func ParseSlackEvent(body []byte) (*SlackEvent, error) {
	var event SlackEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("failed to parse slack event: %w", err)
	}
	return &event, nil
}

// IsURLVerification checks if this is a URL verification challenge.
func (e *SlackEvent) IsURLVerification() bool {
	return e.Type == SlackEventTypeURLVerification
}

// IsAppMention checks if this is an app mention.
func (e *SlackEvent) IsAppMention() bool {
	return e.Type == SlackEventTypeEventCallback && e.Event != nil && e.Event.Type == SlackEventAppMention
}

// IsDirectMessage checks if the message is from a DM channel.
func (e *SlackEvent) IsDirectMessage() bool {
	return e.Event != nil && e.Event.ChannelType == "im"
}

// IsBotMessage checks if the message is from a bot.
func (e *SlackEvent) IsBotMessage() bool {
	return e.Event != nil && (e.Event.BotID != "" || e.Event.SubType == SlackEventBotMessage)
}

// ExtractMessage extracts the message text from an event.
func (e *SlackEvent) ExtractMessage() string {
	if e.Event == nil {
		return ""
	}

	// Handle message_changed subtype
	if e.Event.SubType == SlackEventMessageChanged {
		// The actual message is in a nested structure
		return ""
	}

	return e.Event.Text
}

// ExtractChannelID extracts the channel ID from an event.
func (e *SlackEvent) ExtractChannelID() string {
	if e.Event == nil {
		return ""
	}
	return e.Event.Channel
}

// ExtractUserID extracts the user ID from an event.
func (e *SlackEvent) ExtractUserID() string {
	if e.Event == nil {
		return ""
	}
	return e.Event.User
}

// ExtractThreadTs extracts the thread timestamp for threaded replies.
func (e *SlackEvent) ExtractThreadTs() string {
	if e.Event == nil {
		return ""
	}
	return e.Event.ThreadTs
}

// ============================================================================
// SLACK INTERACTIVE COMPONENTS
// ============================================================================

// SlackInteraction represents an interactive component interaction.
type SlackInteraction struct {
	Type        string            `json:"type"`
	User        *SlackUser        `json:"user"`
	Channel     *SlackChannelInfo `json:"channel"`
	Message     *SlackMessageInfo `json:"message"`
	Actions     []SlackAction     `json:"actions"`
	TriggerID   string            `json:"trigger_id"`
	ResponseURL string            `json:"response_url"`
}

// SlackUser represents a Slack user.
type SlackUser struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	TeamID string `json:"team_id"`
}

// SlackChannelInfo represents channel information.
type SlackChannelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SlackMessageInfo represents message information.
type SlackMessageInfo struct {
	Ts      string `json:"ts"`
	Text    string `json:"text"`
	Channel string `json:"channel"`
}

// SlackAction represents an action from an interactive component.
type SlackAction struct {
	Type     string `json:"type"`
	ActionID string `json:"action_id"`
	Value    string `json:"value"`
	Text     struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"text"`
}

// ParseSlackInteraction parses a Slack interaction payload.
func ParseSlackInteraction(payload string) (*SlackInteraction, error) {
	var interaction SlackInteraction
	if err := json.Unmarshal([]byte(payload), &interaction); err != nil {
		return nil, fmt.Errorf("failed to parse slack interaction: %w", err)
	}
	return &interaction, nil
}

// ============================================================================
// SLACK RESPONSE HELPERS
// ============================================================================

// SlackResponse represents a response to a Slack interaction.
type SlackResponse struct {
	ResponseType    string      `json:"response_type"`
	Text            string      `json:"text"`
	Blocks          []SlackBlock `json:"blocks,omitempty"`
	ReplaceOriginal bool        `json:"replace_original,omitempty"`
}

// NewSlackResponse creates a new in-channel response.
func NewSlackResponse(text string) *SlackResponse {
	return &SlackResponse{
		ResponseType: "in_channel",
		Text:         text,
	}
}

// NewEphemeralSlackResponse creates an ephemeral response.
func NewEphemeralSlackResponse(text string) *SlackResponse {
	return &SlackResponse{
		ResponseType: "ephemeral",
		Text:         text,
	}
}

// ToJSON converts the response to JSON bytes.
func (r *SlackResponse) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// RespondToInteraction responds to a Slack interaction via response_url.
func (c *SlackChannel) RespondToInteraction(responseURL string, response *SlackResponse) error {
	bodyBytes, err := response.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	req, err := http.NewRequest("POST", responseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to respond to interaction: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// RemoveBotMention removes the bot mention from a message.
func RemoveBotMention(text, botUserID string) string {
	// Remove <@BOT_ID> mention
	mention := "<@" + botUserID + ">"
	text = strings.ReplaceAll(text, mention, "")

	// Remove <@BOT_ID|botname> mention format
	text = strings.ReplaceAll(text, "<@"+botUserID+"|", "")
	text = strings.TrimSuffix(text, ">")

	return strings.TrimSpace(text)
}

// IsBotMentioned checks if the bot is mentioned in the message.
func IsBotMentioned(text, botUserID string) bool {
	return strings.Contains(text, "<@"+botUserID+">")
}
