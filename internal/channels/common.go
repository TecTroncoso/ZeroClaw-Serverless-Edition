// Package channels implements communication channels for ZeroClaw.
// This module provides common types and utilities for all channels.
package channels

import (
	"time"
)

// httpTimeout is the default HTTP client timeout for channel requests.
const httpTimeout = 30 * time.Second

// ============================================================================
// CHANNEL INTERFACE
// ============================================================================

// Channel defines the interface for communication channels.
// Each channel (Telegram, Discord, Slack, WhatsApp) implements this interface.
type Channel interface {
	// Name returns the channel identifier (e.g., "telegram", "discord").
	Name() string
	
	// Send delivers a message to a recipient.
	// recipient format depends on the channel:
	//   - Telegram: chat_id (integer as string)
	//   - Discord: channel_id
	//   - Slack: channel_id (e.g., "C1234567890")
	//   - WhatsApp: phone number (e.g., "34612345678")
	Send(recipient, message string) error
}

// ============================================================================
// INCOMING MESSAGE
// ============================================================================

// IncomingMessage represents a normalized incoming message from any channel.
type IncomingMessage struct {
	// Channel identifies the source channel.
	Channel string
	
	// SenderID is the unique identifier of the sender.
	SenderID string
	
	// SenderName is the display name of the sender.
	SenderName string
	
	// ChatID is the conversation/channel ID.
	ChatID string
	
	// Text is the message content.
	Text string
	
	// Timestamp is when the message was sent.
	Timestamp time.Time
	
	// Metadata contains channel-specific data.
	Metadata map[string]string
}

// ============================================================================
// CHANNEL FACTORY
// ============================================================================

// ChannelFactory creates channels by name.
type ChannelFactory struct {
	// No state needed - all channels read from env vars
}

// NewChannelFactory creates a new channel factory.
func NewChannelFactory() *ChannelFactory {
	return &ChannelFactory{}
}

// Create creates a channel by name.
func (f *ChannelFactory) Create(channelName string) Channel {
	switch channelName {
	case "telegram":
		return NewTelegramChannel()
	case "discord":
		return NewDiscordChannel()
	case "slack":
		return NewSlackChannel()
	case "whatsapp":
		return NewWhatsAppChannel()
	default:
		return nil
	}
}

// CreateAll creates all configured channels.
func (f *ChannelFactory) CreateAll() map[string]Channel {
	channels := make(map[string]Channel)
	
	if ch := f.Create("telegram"); ch != nil {
		channels["telegram"] = ch
	}
	if ch := f.Create("discord"); ch != nil {
		channels["discord"] = ch
	}
	if ch := f.Create("slack"); ch != nil {
		channels["slack"] = ch
	}
	if ch := f.Create("whatsapp"); ch != nil {
		channels["whatsapp"] = ch
	}
	
	return channels
}

// ============================================================================
// MESSAGE BUILDER
// ============================================================================

// MessageBuilder helps construct messages with formatting.
type MessageBuilder struct {
	parts []string
}

// NewMessageBuilder creates a new message builder.
func NewMessageBuilder() *MessageBuilder {
	return &MessageBuilder{
		parts: make([]string, 0),
	}
}

// Add adds a text segment.
func (b *MessageBuilder) Add(text string) *MessageBuilder {
	b.parts = append(b.parts, text)
	return b
}

// AddLine adds a text segment with newline.
func (b *MessageBuilder) AddLine(text string) *MessageBuilder {
	b.parts = append(b.parts, text+"\n")
	return b
}

// AddBold adds bold text (platform-dependent).
func (b *MessageBuilder) AddBold(text string) *MessageBuilder {
	b.parts = append(b.parts, "**"+text+"**")
	return b
}

// AddItalic adds italic text.
func (b *MessageBuilder) AddItalic(text string) *MessageBuilder {
	b.parts = append(b.parts, "*"+text+"*")
	return b
}

// AddCode adds inline code.
func (b *MessageBuilder) AddCode(text string) *MessageBuilder {
	b.parts = append(b.parts, "`"+text+"`")
	return b
}

// AddCodeBlock adds a code block.
func (b *MessageBuilder) AddCodeBlock(language, code string) *MessageBuilder {
	b.parts = append(b.parts, "```"+language+"\n"+code+"\n```")
	return b
}

// AddLink adds a hyperlink.
func (b *MessageBuilder) AddLink(text, url string) *MessageBuilder {
	b.parts = append(b.parts, "["+text+"]("+url+")")
	return b
}

// Newline adds a newline.
func (b *MessageBuilder) Newline() *MessageBuilder {
	b.parts = append(b.parts, "\n")
	return b
}

// Build returns the final message.
func (b *MessageBuilder) Build() string {
	return joinParts(b.parts)
}

// BuildTruncated returns the message truncated to maxLen.
func (b *MessageBuilder) BuildTruncated(maxLen int) string {
	msg := b.Build()
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen-3] + "..."
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func joinParts(parts []string) string {
	result := ""
	for _, part := range parts {
		result += part
	}
	return result
}
