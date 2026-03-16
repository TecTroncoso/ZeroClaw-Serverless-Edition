// Package channels implements communication channels for ZeroClaw.
// This module provides WhatsApp Cloud API integration via Webhook.
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
// WHATSAPP CHANNEL
// ============================================================================

// WhatsAppChannel implements the Channel interface for WhatsApp.
type WhatsAppChannel struct {
	accessToken string
	phoneID     string
	httpClient  *http.Client
}

// NewWhatsAppChannel creates a new WhatsApp channel.
func NewWhatsAppChannel() *WhatsAppChannel {
	return NewWhatsAppChannelWithConfig(
		os.Getenv("WHATSAPP_TOKEN"),
		os.Getenv("WHATSAPP_PHONE_ID"),
	)
}

// NewWhatsAppChannelWithConfig creates a WhatsApp channel with explicit config.
func NewWhatsAppChannelWithConfig(accessToken, phoneID string) *WhatsAppChannel {
	return &WhatsAppChannel{
		accessToken: accessToken,
		phoneID:     phoneID,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
	}
}

// Name returns the channel name.
func (c *WhatsAppChannel) Name() string {
	return "whatsapp"
}

// Send sends a message to a WhatsApp user.
// recipient should be the phone number in international format (e.g., "34612345678").
func (c *WhatsAppChannel) Send(recipient, message string) error {
	if c.accessToken == "" {
		return fmt.Errorf("WHATSAPP_TOKEN not configured")
	}
	if c.phoneID == "" {
		return fmt.Errorf("WHATSAPP_PHONE_ID not configured")
	}

	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", c.phoneID)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                recipient,
		"type":              "text",
		"text": map[string]string{
			"preview_url": "false",
			"body":        message,
		},
	}

	return c.doRequest(url, payload)
}

// SendMarkdown sends a message with basic markdown formatting.
func (c *WhatsAppChannel) SendMarkdown(recipient, message string) error {
	// WhatsApp doesn't support markdown, convert to plain text
	plainText := c.stripMarkdown(message)
	return c.Send(recipient, plainText)
}

// SendInteractive sends an interactive message with buttons.
func (c *WhatsAppChannel) SendInteractive(recipient string, content WhatsAppInteractiveContent) error {
	if c.accessToken == "" {
		return fmt.Errorf("WHATSAPP_TOKEN not configured")
	}
	if c.phoneID == "" {
		return fmt.Errorf("WHATSAPP_PHONE_ID not configured")
	}

	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", c.phoneID)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                recipient,
		"type":              "interactive",
		"interactive":       content,
	}

	return c.doRequest(url, payload)
}

// WhatsAppInteractiveContent represents interactive message content.
type WhatsAppInteractiveContent struct {
	Type   string                    `json:"type"`
	Body   *WhatsAppInteractiveBody  `json:"body,omitempty"`
	Action *WhatsAppInteractiveAction `json:"action,omitempty"`
}

// WhatsAppInteractiveBody represents the body of an interactive message.
type WhatsAppInteractiveBody struct {
	Text string `json:"text"`
}

// WhatsAppInteractiveAction represents action buttons.
type WhatsAppInteractiveAction struct {
	Button  string          `json:"button,omitempty"`
	Buttons []WhatsAppButton `json:"buttons,omitempty"`
}

// WhatsAppButton represents a button.
type WhatsAppButton struct {
	Type  string              `json:"type"`
	Reply *WhatsAppButtonReply `json:"reply,omitempty"`
}

// WhatsAppButtonReply represents button reply data.
type WhatsAppButtonReply struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// SendTemplate sends a template message.
func (c *WhatsAppChannel) SendTemplate(recipient, templateName, language string, components []WhatsAppTemplateComponent) error {
	if c.accessToken == "" {
		return fmt.Errorf("WHATSAPP_TOKEN not configured")
	}
	if c.phoneID == "" {
		return fmt.Errorf("WHATSAPP_PHONE_ID not configured")
	}

	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", c.phoneID)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                recipient,
		"type":              "template",
		"template": map[string]interface{}{
			"name": templateName,
			"language": map[string]string{"code": language},
			"components": components,
		},
	}

	return c.doRequest(url, payload)
}

// WhatsAppTemplateComponent represents a template component.
type WhatsAppTemplateComponent struct {
	Type       string                 `json:"type"`
	Parameters []WhatsAppTemplateParam `json:"parameters,omitempty"`
}

// WhatsAppTemplateParam represents a template parameter.
type WhatsAppTemplateParam struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// SendTyping sends a typing indicator (Not natively supported in WhatsApp Cloud API).
func (c *WhatsAppChannel) SendTyping(ctx context.Context, recipient string) error {
	// WhatsApp Business Cloud API does not support "typing..." indicators.
	return nil
}

// ============================================================================
// HTTP REQUEST
// ============================================================================

func (c *WhatsAppChannel) doRequest(url string, payload interface{}) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("whatsapp API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ============================================================================
// WHATSAPP WEBHOOK PARSING
// ============================================================================

// WhatsAppWebhook represents an incoming WhatsApp webhook.
type WhatsAppWebhook struct {
	Object string `json:"object"`
	Entry []struct {
		ID      string `json:"id"`
		Changes []struct {
			Value WhatsAppValue `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

// WhatsAppValue contains the message data.
type WhatsAppValue struct {
	MessagingProduct string          `json:"messaging_product"`
	Metadata *struct {
		DisplayPhoneNumber string `json:"display_phone_number"`
		PhoneNumberID      string `json:"phone_number_id"`
	} `json:"metadata"`
	Contacts []WhatsAppContact `json:"contacts"`
	Messages []WhatsAppMessage `json:"messages"`
	Statuses []WhatsAppStatus  `json:"statuses"`
}

// WhatsAppContact represents a contact.
type WhatsAppContact struct {
	Profile *struct {
		Name string `json:"name"`
	} `json:"profile"`
	WaID string `json:"wa_id"`
}

// WhatsAppMessage represents a message.
type WhatsAppMessage struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Text *struct {
		Body string `json:"body"`
	} `json:"text"`
	Image *struct {
		ID       string `json:"id"`
		MimeType string `json:"mime_type"`
		Caption  string `json:"caption"`
	} `json:"image"`
	Audio *struct {
		ID       string `json:"id"`
		MimeType string `json:"mime_type"`
	} `json:"audio"`
	Video *struct {
		ID       string `json:"id"`
		MimeType string `json:"mime_type"`
		Caption  string `json:"caption"`
	} `json:"video"`
	Document *struct {
		ID       string `json:"id"`
		MimeType string `json:"mime_type"`
		Filename string `json:"filename"`
		Caption  string `json:"caption"`
	} `json:"document"`
	Location *struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Name      string  `json:"name"`
		Address   string  `json:"address"`
	} `json:"location"`
	Interactive *struct {
		Type string `json:"type"`
		ButtonReply *struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"button_reply"`
		ListReply *struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"list_reply"`
	} `json:"interactive"`
	Context *struct {
		Forwarded bool   `json:"forwarded"`
		From      string `json:"from"`
		ID        string `json:"id"`
	} `json:"context"`
}

// WhatsAppStatus represents a message status update.
type WhatsAppStatus struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Timestamp    string `json:"timestamp"`
	RecipientID  string `json:"recipient_id"`
	Conversation *struct {
		ID string `json:"id"`
		Origin *struct {
			Type string `json:"type"`
		} `json:"origin"`
	} `json:"conversation"`
	Pricing *struct {
		Billable bool   `json:"billable"`
		Model    string `json:"pricing_model"`
		Category string `json:"category"`
	} `json:"pricing"`
}

// Message types
const (
	WhatsAppTypeText        = "text"
	WhatsAppTypeImage       = "image"
	WhatsAppTypeAudio       = "audio"
	WhatsAppTypeVideo       = "video"
	WhatsAppTypeDocument    = "document"
	WhatsAppTypeLocation    = "location"
	WhatsAppTypeInteractive = "interactive"
)

// Status types
const (
	WhatsAppStatusSent      = "sent"
	WhatsAppStatusDelivered = "delivered"
	WhatsAppStatusRead      = "read"
	WhatsAppStatusFailed    = "failed"
)

// ParseWhatsAppWebhook parses a WhatsApp webhook request.
func ParseWhatsAppWebhook(body []byte) (*WhatsAppWebhook, error) {
	var webhook WhatsAppWebhook
	if err := json.Unmarshal(body, &webhook); err != nil {
		return nil, fmt.Errorf("failed to parse whatsapp webhook: %w", err)
	}
	return &webhook, nil
}

// HasMessages checks if the webhook contains messages.
func (w *WhatsAppWebhook) HasMessages() bool {
	if len(w.Entry) == 0 {
		return false
	}
	for _, entry := range w.Entry {
		for _, change := range entry.Changes {
			if len(change.Value.Messages) > 0 {
				return true
			}
		}
	}
	return false
}

// ExtractFirstMessage extracts the first message from the webhook.
func (w *WhatsAppWebhook) ExtractFirstMessage() (*WhatsAppMessage, *WhatsAppContact) {
	for _, entry := range w.Entry {
		for _, change := range entry.Changes {
			if len(change.Value.Messages) > 0 {
				msg := &change.Value.Messages[0]
				var contact *WhatsAppContact
				if len(change.Value.Contacts) > 0 {
					contact = &change.Value.Contacts[0]
				}
				return msg, contact
			}
		}
	}
	return nil, nil
}

// ExtractMessageText extracts the text content from a message.
func (m *WhatsAppMessage) ExtractMessageText() string {
	switch m.Type {
	case WhatsAppTypeText:
		if m.Text != nil {
			return m.Text.Body
		}
	case WhatsAppTypeInteractive:
		if m.Interactive != nil {
			if m.Interactive.ButtonReply != nil {
				return m.Interactive.ButtonReply.Title
			}
			if m.Interactive.ListReply != nil {
				return m.Interactive.ListReply.Title
			}
		}
	case WhatsAppTypeImage:
		if m.Image != nil {
			if m.Image.Caption != "" {
				return "[Image: " + m.Image.Caption + "]"
			}
			return "[Image]"
		}
	case WhatsAppTypeVideo:
		if m.Video != nil {
			if m.Video.Caption != "" {
				return "[Video: " + m.Video.Caption + "]"
			}
			return "[Video]"
		}
	case WhatsAppTypeDocument:
		if m.Document != nil {
			if m.Document.Caption != "" {
				return "[Document: " + m.Document.Caption + "]"
			}
			return "[Document: " + m.Document.Filename + "]"
		}
	case WhatsAppTypeLocation:
		if m.Location != nil {
			return "[Location: " + m.Location.Name + "]"
		}
	}
	return ""
}

// ExtractSenderPhone extracts the sender's phone number.
func (m *WhatsAppMessage) ExtractSenderPhone() string {
	return m.From
}

// ExtractSenderName extracts the sender's name.
func (c *WhatsAppContact) ExtractSenderName() string {
	if c.Profile != nil {
		return c.Profile.Name
	}
	return ""
}

// IsStatusUpdate checks if the webhook is a status update.
func (w *WhatsAppWebhook) IsStatusUpdate() bool {
	for _, entry := range w.Entry {
		for _, change := range entry.Changes {
			if len(change.Value.Statuses) > 0 {
				return true
			}
		}
	}
	return false
}

// ============================================================================
// MARKDOWN HELPER
// ============================================================================

// stripMarkdown removes markdown formatting from text.
func (c *WhatsAppChannel) stripMarkdown(text string) string {
	// Remove bold
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "__", "")

	// Remove italic
	text = strings.ReplaceAll(text, "*", "")
	text = strings.ReplaceAll(text, "_", "")

	// Remove code blocks
	text = strings.ReplaceAll(text, "```", "")
	text = strings.ReplaceAll(text, "`", "")

	// Remove links but keep text
	for {
		start := strings.Index(text, "[")
		if start == -1 {
			break
		}
		end := strings.Index(text[start:], "]")
		if end == -1 {
			break
		}
		linkText := text[start+1 : start+end]
		rest := text[start+end+1:]
		parenStart := strings.Index(rest, "(")
		if parenStart != 0 {
			break
		}
		parenEnd := strings.Index(rest, ")")
		if parenEnd == -1 {
			break
		}
		text = text[:start] + linkText + rest[parenEnd+1:]
	}

	return strings.TrimSpace(text)
}

// ============================================================================
// MARK MESSAGE AS READ
// ============================================================================

// MarkAsRead marks a message as read.
func (c *WhatsAppChannel) MarkAsRead(messageID string) error {
	if c.accessToken == "" {
		return fmt.Errorf("WHATSAPP_TOKEN not configured")
	}
	if c.phoneID == "" {
		return fmt.Errorf("WHATSAPP_PHONE_ID not configured")
	}

	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", c.phoneID)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"status":            "read",
		"message_id":        messageID,
	}

	return c.doRequest(url, payload)
}
