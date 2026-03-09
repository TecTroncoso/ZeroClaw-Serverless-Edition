// Package channels implements communication channels for ZeroClaw.
// This module provides Discord bot integration via Interactions Webhook.
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
// DISCORD CHANNEL
// ============================================================================

// DiscordChannel implements the Channel interface for Discord.
type DiscordChannel struct {
	botToken   string
	httpClient *http.Client
}

// NewDiscordChannel creates a new Discord channel.
func NewDiscordChannel() *DiscordChannel {
	return NewDiscordChannelWithToken(os.Getenv("DISCORD_BOT_TOKEN"))
}

// NewDiscordChannelWithToken creates a Discord channel with explicit token.
func NewDiscordChannelWithToken(token string) *DiscordChannel {
	return &DiscordChannel{
		botToken: token,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
	}
}

// Name returns the channel name.
func (c *DiscordChannel) Name() string {
	return "discord"
}

// Send sends a message to a Discord channel.
// recipient should be "channel_id" format.
func (c *DiscordChannel) Send(recipient, message string) error {
	if c.botToken == "" {
		return fmt.Errorf("DISCORD_BOT_TOKEN not configured")
	}

	// Discord API v10 endpoint
	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", recipient)

	payload := map[string]interface{}{
		"content": truncateMessage(message, 2000), // Discord limit
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
	req.Header.Set("Authorization", "Bot "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SendEmbed sends a rich embed message to a Discord channel.
func (c *DiscordChannel) SendEmbed(recipient string, embed DiscordEmbed) error {
	if c.botToken == "" {
		return fmt.Errorf("DISCORD_BOT_TOKEN not configured")
	}

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", recipient)

	payload := map[string]interface{}{
		"embeds": []DiscordEmbed{embed},
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
	req.Header.Set("Authorization", "Bot "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send embed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// DiscordEmbed represents a Discord embed object.
type DiscordEmbed struct {
	Title       string             `json:"title,omitempty"`
	Description string             `json:"description,omitempty"`
	URL         string             `json:"url,omitempty"`
	Color       int                `json:"color,omitempty"`
	Fields      []DiscordEmbedField `json:"fields,omitempty"`
	Footer      *DiscordEmbedFooter `json:"footer,omitempty"`
	Author      *DiscordEmbedAuthor `json:"author,omitempty"`
}

// DiscordEmbedField represents a field in an embed.
type DiscordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// DiscordEmbedFooter represents an embed footer.
type DiscordEmbedFooter struct {
	Text    string `json:"text"`
	IconURL string `json:"icon_url,omitempty"`
}

// DiscordEmbedAuthor represents an embed author.
type DiscordEmbedAuthor struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	IconURL string `json:"icon_url,omitempty"`
}

// ============================================================================
// DISCORD INTERACTIONS WEBHOOK PARSING
// ============================================================================

// DiscordInteraction represents an incoming Discord interaction.
type DiscordInteraction struct {
	Type          int                   `json:"type"`
	Token         string                `json:"token"`
	Data          *DiscordInteractionData `json:"data"`
	ID            string                `json:"id"`
	ApplicationID string                `json:"application_id"`
	GuildID       string                `json:"guild_id,omitempty"`
	ChannelID     string                `json:"channel_id"`
	User          *DiscordUser          `json:"user"`
	Member        *DiscordMember        `json:"member,omitempty"`
	Message       *DiscordMessage       `json:"message,omitempty"`
}

// DiscordInteractionData contains the command/interaction data.
type DiscordInteractionData struct {
	Type       int                       `json:"type"`
	Name       string                    `json:"name"`
	ID         string                    `json:"id"`
	Options    []DiscordInteractionOption `json:"options,omitempty"`
	CustomID   string                    `json:"custom_id,omitempty"`
	Components []DiscordComponent        `json:"components,omitempty"`
}

// DiscordInteractionOption represents a command option.
type DiscordInteractionOption struct {
	Name    string                  `json:"name"`
	Type    int                     `json:"type"`
	Value   interface{}             `json:"value"`
	Options []DiscordInteractionOption `json:"options,omitempty"`
}

// DiscordComponent represents a message component.
type DiscordComponent struct {
	Type     int    `json:"type"`
	CustomID string `json:"custom_id,omitempty"`
	Value    string `json:"value,omitempty"`
}

// DiscordUser represents a Discord user.
type DiscordUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	GlobalName    string `json:"global_name"`
	Avatar        string `json:"avatar"`
}

// DiscordMember represents a guild member.
type DiscordMember struct {
	User  DiscordUser `json:"user"`
	Nick  string      `json:"nick,omitempty"`
	Roles []string    `json:"roles,omitempty"`
}

// DiscordMessage represents a Discord message.
type DiscordMessage struct {
	ID        string      `json:"id"`
	Content   string      `json:"content"`
	Author    DiscordUser `json:"author"`
	ChannelID string      `json:"channel_id"`
	GuildID   string      `json:"guild_id,omitempty"`
}

// Interaction types
const (
	DiscordInteractionPing       = 1
	DiscordInteractionApplication = 2
	DiscordInteractionMessageComp = 3
	DiscordInteractionModalSubmit = 5
)

// Application command types
const (
	DiscordCommandChatInput = 1
	DiscordCommandUser      = 2
	DiscordCommandMessage   = 3
)

// ParseDiscordInteraction parses a Discord interaction request.
func ParseDiscordInteraction(body []byte) (*DiscordInteraction, error) {
	var interaction DiscordInteraction
	if err := json.Unmarshal(body, &interaction); err != nil {
		return nil, fmt.Errorf("failed to parse discord interaction: %w", err)
	}
	return &interaction, nil
}

// IsPing checks if the interaction is a ping (health check).
func (i *DiscordInteraction) IsPing() bool {
	return i.Type == DiscordInteractionPing
}

// ExtractMessage extracts the message text from an interaction.
func (i *DiscordInteraction) ExtractMessage() string {
	// Application command
	if i.Data != nil && i.Data.Name != "" {
		// Build command string with options
		parts := []string{"/" + i.Data.Name}
		for _, opt := range i.Data.Options {
			parts = append(parts, fmt.Sprintf("%v", opt.Value))
		}
		return strings.Join(parts, " ")
	}

	// Message component (button, select menu)
	if i.Data != nil && i.Data.CustomID != "" {
		return i.Data.CustomID
	}

	// Modal submit
	if i.Data != nil && len(i.Data.Components) > 0 {
		var values []string
		for _, comp := range i.Data.Components {
			if comp.Value != "" {
				values = append(values, comp.Value)
			}
		}
		return strings.Join(values, " ")
	}

	return ""
}

// ExtractChannelID extracts the channel ID from an interaction.
func (i *DiscordInteraction) ExtractChannelID() string {
	return i.ChannelID
}

// ExtractUserID extracts the user ID from an interaction.
func (i *DiscordInteraction) ExtractUserID() string {
	if i.Member != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

// ExtractUserName extracts the user's display name.
func (i *DiscordInteraction) ExtractUserName() string {
	if i.Member != nil {
		if i.Member.Nick != "" {
			return i.Member.Nick
		}
		return i.Member.User.GlobalName
	}
	if i.User != nil {
		return i.User.GlobalName
	}
	return ""
}

// ============================================================================
// DISCORD INTERACTION RESPONSE
// ============================================================================

// DiscordInteractionResponse represents a response to an interaction.
type DiscordInteractionResponse struct {
	Type int                        `json:"type"`
	Data *DiscordInteractionResponseData `json:"data,omitempty"`
}

// DiscordInteractionResponseData contains the response data.
type DiscordInteractionResponseData struct {
	Content string        `json:"content,omitempty"`
	Embeds  []DiscordEmbed `json:"embeds,omitempty"`
	Flags   int           `json:"flags,omitempty"`
}

// Response types
const (
	DiscordResponsePong             = 1
	DiscordResponseChannelMessage   = 4
	DiscordResponseDeferredMessage  = 5
	DiscordResponseUpdateMessage    = 7
	DiscordResponseDeferredUpdate   = 6
)

// Response flags
const (
	DiscordFlagEphemeral = 1 << 6
)

// NewPongResponse creates a pong response for ping interactions.
func NewPongResponse() *DiscordInteractionResponse {
	return &DiscordInteractionResponse{
		Type: DiscordResponsePong,
	}
}

// NewMessageResponse creates a channel message response.
func NewMessageResponse(content string) *DiscordInteractionResponse {
	return &DiscordInteractionResponse{
		Type: DiscordResponseChannelMessage,
		Data: &DiscordInteractionResponseData{
			Content: truncateMessage(content, 2000),
		},
	}
}

// NewEphemeralResponse creates an ephemeral message (only visible to user).
func NewEphemeralResponse(content string) *DiscordInteractionResponse {
	return &DiscordInteractionResponse{
		Type: DiscordResponseChannelMessage,
		Data: &DiscordInteractionResponseData{
			Content: truncateMessage(content, 2000),
			Flags:   DiscordFlagEphemeral,
		},
	}
}

// NewDeferredResponse creates a deferred response (shows "thinking...").
func NewDeferredResponse() *DiscordInteractionResponse {
	return &DiscordInteractionResponse{
		Type: DiscordResponseDeferredMessage,
	}
}

// ToJSON converts the response to JSON bytes.
func (r *DiscordInteractionResponse) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// ============================================================================
// FOLLOWUP MESSAGE (for deferred responses)
// ============================================================================

// SendFollowup sends a followup message after a deferred response.
func (c *DiscordChannel) SendFollowup(applicationID, interactionToken, content string) error {
	if c.botToken == "" {
		return fmt.Errorf("DISCORD_BOT_TOKEN not configured")
	}

	url := fmt.Sprintf("https://discord.com/api/v10/webhooks/%s/%s", applicationID, interactionToken)

	payload := map[string]interface{}{
		"content": truncateMessage(content, 2000),
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to send followup: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// EditOriginalResponse edits the original interaction response.
func (c *DiscordChannel) EditOriginalResponse(applicationID, interactionToken, content string) error {
	if c.botToken == "" {
		return fmt.Errorf("DISCORD_BOT_TOKEN not configured")
	}

	url := fmt.Sprintf("https://discord.com/api/v10/webhooks/%s/%s/messages/@original", applicationID, interactionToken)

	payload := map[string]interface{}{
		"content": truncateMessage(content, 2000),
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("PATCH", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to edit response: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// truncateMessage truncates a message to fit within Discord's limits.
func truncateMessage(message string, maxLength int) string {
	if len(message) <= maxLength {
		return message
	}
	return message[:maxLength-3] + "..."
}
