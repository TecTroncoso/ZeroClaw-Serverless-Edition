package tools

import (
	"context"
	"fmt"
	"net/smtp"
	"os"

	"github.com/zeroclaw/zeroclaw-go/pkg/core"
)

type SendEmailTool struct{}

func NewSendEmailTool() *SendEmailTool {
	return &SendEmailTool{}
}

func (t *SendEmailTool) Name() string {
	return "send_email"
}

func (t *SendEmailTool) Description() string {
	return "Sends an email via SMTP."
}

func (t *SendEmailTool) ParametersSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"to": map[string]interface{}{
				"type":        "string",
				"description": "Recipient email address.",
			},
			"subject": map[string]interface{}{
				"type":        "string",
				"description": "Subject of the email.",
			},
			"body": map[string]interface{}{
				"type":        "string",
				"description": "Body content of the email.",
			},
		},
		"required": []string{"to", "subject", "body"},
	}
}

func (t *SendEmailTool) Execute(ctx context.Context, args map[string]interface{}) (*core.ToolResult, error) {
	to, _ := args["to"].(string)
	subject, _ := args["subject"].(string)
	body, _ := args["body"].(string)

	if to == "" || subject == "" || body == "" {
		return core.NewErrorResult("Missing required arguments: to, subject, or body"), nil
	}

	user := os.Getenv("SMTP_USER")
	password := os.Getenv("SMTP_PASSWORD")
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		host = "smtp.gmail.com"
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}

	if user == "" || password == "" {
		return core.NewErrorResult("SMTP configuration missing: SMTP_USER or SMTP_PASSWORD is not set"), nil
	}

	auth := smtp.PlainAuth("", user, password, host)

	// Minimal headers and content construction avoiding duplicate To/Subject lines
	msg := []byte("To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"\r\n" +
		body + "\r\n")

	err := smtp.SendMail(host+":"+port, auth, user, []string{to}, msg)
	if err != nil {
		return core.NewErrorResult(fmt.Sprintf("Failed to send email: %v", err)), nil
	}

	return core.NewSuccessResult("Email sent successfully"), nil
}

func (t *SendEmailTool) Spec() core.ToolSpec {
	return core.ToolSpec{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.ParametersSchema(),
	}
}
