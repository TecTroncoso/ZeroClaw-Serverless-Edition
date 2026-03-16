// Package channels implements communication channels for ZeroClaw.
// This module provides Telegram bot integration via Webhook.
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
// TELEGRAM CHANNEL
// ============================================================================

// TelegramChannel implements the Channel interface for Telegram.
type TelegramChannel struct {
	botToken  string
	httpClient *http.Client
}

// NewTelegramChannel creates a new Telegram channel.
func NewTelegramChannel() *TelegramChannel {
	return NewTelegramChannelWithToken(os.Getenv("TELEGRAM_BOT_TOKEN"))
}

// NewTelegramChannelWithToken creates a Telegram channel with explicit token.
func NewTelegramChannelWithToken(token string) *TelegramChannel {
	return &TelegramChannel{
		botToken: token,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
	}
}

// Name returns the channel name.
func (c *TelegramChannel) Name() string {
	return "telegram"
}

// Send sends a message to a Telegram chat.
// recipient should be the chat_id (integer as string).
func (c *TelegramChannel) Send(recipient, message string) error {
	if c.botToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN not configured")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.botToken)

	payload := map[string]interface{}{
		"chat_id":    recipient,
		"text":       message,
		"parse_mode": "Markdown",
		"disable_web_page_preview": true,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		errBody := string(respBody)
		if strings.Contains(errBody, "can't parse entities") {
			// Fallback: Retry with empty ParseMode
			payload["parse_mode"] = ""
			retryBytes, _ := json.Marshal(payload)
			retryResp, retryErr := c.httpClient.Post(url, "application/json", bytes.NewReader(retryBytes))
			if retryErr != nil {
				return fmt.Errorf("failed to retry send message: %w", retryErr)
			}
			defer retryResp.Body.Close()
			
			retryBody, _ := io.ReadAll(retryResp.Body)
			if retryResp.StatusCode >= 400 {
				return fmt.Errorf("telegram API error on retry (status %d): %s", retryResp.StatusCode, string(retryBody))
			}
			return nil
		}
		return fmt.Errorf("telegram API error (status %d): %s", resp.StatusCode, errBody)
	}

	return nil
}

// SendWithKeyboard sends a message with an inline keyboard.
func (c *TelegramChannel) SendWithKeyboard(recipient, message string, buttons [][]TelegramButton) error {
	if c.botToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN not configured")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.botToken)

	payload := map[string]interface{}{
		"chat_id":    recipient,
		"text":       message,
		"parse_mode": "Markdown",
		"disable_web_page_preview": true,
		"reply_markup": map[string]interface{}{
			"inline_keyboard": buttons,
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		errBody := string(respBody)
		if strings.Contains(errBody, "can't parse entities") {
			// Fallback: Retry with empty ParseMode
			payload["parse_mode"] = ""
			retryBytes, _ := json.Marshal(payload)
			retryResp, retryErr := c.httpClient.Post(url, "application/json", bytes.NewReader(retryBytes))
			if retryErr != nil {
				return fmt.Errorf("failed to retry send message: %w", retryErr)
			}
			defer retryResp.Body.Close()
			
			retryBody, _ := io.ReadAll(retryResp.Body)
			if retryResp.StatusCode >= 400 {
				return fmt.Errorf("telegram API error on retry (status %d): %s", retryResp.StatusCode, string(retryBody))
			}
			return nil
		}
		return fmt.Errorf("telegram API error (status %d): %s", resp.StatusCode, errBody)
	}

	return nil
}

// TelegramButton represents an inline keyboard button.
type TelegramButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

// SendTyping sends a "typing..." action to the Telegram chat.
func (c *TelegramChannel) SendTyping(ctx context.Context, recipient string) error {
	if c.botToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN not configured")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendChatAction", c.botToken)

	payload := map[string]interface{}{
		"chat_id": recipient,
		"action":  "typing",
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send typing action: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	return nil
}

// ============================================================================
// TELEGRAM WEBHOOK PARSING
// ============================================================================

// TelegramWebhook represents an incoming Telegram update.
type TelegramWebhook struct {
	UpdateID int `json:"update_id"`
	Message *struct {
		MessageID int `json:"message_id"`
		From *struct {
			ID        int64  `json:"id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Username  string `json:"username"`
		} `json:"from"`
		Chat *struct {
			ID       int64  `json:"id"`
			Type     string `json:"type"`
			Title    string `json:"title"`
			Username string `json:"username"`
		} `json:"chat"`
		Text string `json:"text"`
		Entities []struct {
			Type   string `json:"type"`
			Offset int    `json:"offset"`
			Length int    `json:"length"`
		} `json:"entities"`
	} `json:"message"`
	CallbackQuery *struct {
		ID string `json:"id"`
		From *struct {
			ID        int64  `json:"id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Username  string `json:"username"`
		} `json:"from"`
		Message *struct {
			MessageID int `json:"message_id"`
			Chat *struct {
				ID       int64  `json:"id"`
				Type     string `json:"type"`
				Title    string `json:"title"`
				Username string `json:"username"`
			} `json:"chat"`
		} `json:"message"`
		Data string `json:"data"`
	} `json:"callback_query"`
}

// ParseTelegramWebhook parses a Telegram webhook request.
func ParseTelegramWebhook(body []byte) (*TelegramWebhook, error) {
	var update TelegramWebhook
	if err := json.Unmarshal(body, &update); err != nil {
		return nil, fmt.Errorf("failed to parse telegram webhook: %w", err)
	}
	return &update, nil
}

// ExtractMessage extracts the message text from a Telegram update.
func (u *TelegramWebhook) ExtractMessage() string {
	if u.Message != nil && u.Message.Text != "" {
		return u.Message.Text
	}
	if u.CallbackQuery != nil && u.CallbackQuery.Data != "" {
		return u.CallbackQuery.Data
	}
	return ""
}

// ExtractSenderID extracts the sender ID from a Telegram update.
func (u *TelegramWebhook) ExtractSenderID() string {
	if u.Message != nil && u.Message.From != nil {
		return fmt.Sprintf("%d", u.Message.From.ID)
	}
	if u.CallbackQuery != nil && u.CallbackQuery.From != nil {
		return fmt.Sprintf("%d", u.CallbackQuery.From.ID)
	}
	return ""
}

// ExtractChatID extracts the chat ID from a Telegram update.
func (u *TelegramWebhook) ExtractChatID() string {
	if u.Message != nil && u.Message.Chat != nil {
		return fmt.Sprintf("%d", u.Message.Chat.ID)
	}
	if u.CallbackQuery != nil && u.CallbackQuery.Message != nil && u.CallbackQuery.Message.Chat != nil {
		return fmt.Sprintf("%d", u.CallbackQuery.Message.Chat.ID)
	}
	return ""
}

// ExtractSenderName extracts the sender's display name.
func (u *TelegramWebhook) ExtractSenderName() string {
	if u.Message != nil && u.Message.From != nil {
		return u.buildUserName(u.Message.From.FirstName, u.Message.From.LastName, u.Message.From.Username)
	}
	if u.CallbackQuery != nil && u.CallbackQuery.From != nil {
		return u.buildUserName(u.CallbackQuery.From.FirstName, u.CallbackQuery.From.LastName, u.CallbackQuery.From.Username)
	}
	return ""
}

func (u *TelegramWebhook) buildUserName(first, last, username string) string {
	if username != "" {
		return "@" + username
	}
	if first != "" && last != "" {
		return first + " " + last
	}
	return first
}

// IsCommand checks if the message is a bot command.
func (u *TelegramWebhook) IsCommand() bool {
	if u.Message == nil || u.Message.Text == "" {
		return false
	}
	return strings.HasPrefix(u.Message.Text, "/")
}

// ExtractCommand extracts the command and arguments from a message.
func (u *TelegramWebhook) ExtractCommand() (command string, args string) {
	if !u.IsCommand() {
		return "", ""
	}

	text := u.Message.Text
	parts := strings.SplitN(text, " ", 2)
	command = strings.ToLower(parts[0])
	if len(parts) > 1 {
		args = parts[1]
	}
	return command, args
}

// AnswerCallbackQuery answers a callback query to remove the loading state.
func (c *TelegramChannel) AnswerCallbackQuery(callbackID, text string) error {
	if c.botToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN not configured")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", c.botToken)

	payload := map[string]interface{}{
		"callback_query_id": callbackID,
		"text":              text,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

// SetWebhook sets the webhook URL for the bot.
func (c *TelegramChannel) SetWebhook(webhookURL string) error {
	if c.botToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN not configured")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/setWebhook", c.botToken)

	payload := map[string]interface{}{
		"url":            webhookURL,
		"allowed_updates": []string{"message", "callback_query"},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to set webhook: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram API error: %s", string(respBody))
	}

	return nil
}
