package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/zeroclaw/zeroclaw-go/pkg/core"
)

type MakeCalendarTool struct{}

func NewMakeCalendarTool() *MakeCalendarTool {
	return &MakeCalendarTool{}
}

func (t *MakeCalendarTool) Name() string {
	return "schedule_calendar_event"
}

func (t *MakeCalendarTool) Description() string {
	return "Schedules a calendar event by triggering a Make.com webhook."
}

func (t *MakeCalendarTool) ParametersSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type":        "string",
				"description": "Title of the calendar event.",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Description of the calendar event.",
			},
			"start_date": map[string]interface{}{
				"type":        "string",
				"description": "Start date and time of the event (YYYY-MM-DD HH:MM).",
			},
			"end_date": map[string]interface{}{
				"type":        "string",
				"description": "End date and time of the event (YYYY-MM-DD HH:MM).",
			},
		},
		"required": []string{"title", "description", "start_date", "end_date"},
	}
}

func (t *MakeCalendarTool) Execute(ctx context.Context, args map[string]interface{}) (*core.ToolResult, error) {
	webhookURL := os.Getenv("MAKE_CALENDAR_WEBHOOK")
	if webhookURL == "" {
		return core.NewErrorResult("Falta configurar el webhook: MAKE_CALENDAR_WEBHOOK is not set."), nil
	}

	// The args map already contains title, description, start_date, end_date
	payload, err := json.Marshal(args)
	if err != nil {
		return core.NewErrorResult(fmt.Sprintf("Failed to marshal webhook payload: %v", err)), nil
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(payload))
	if err != nil {
		return core.NewErrorResult(fmt.Sprintf("Failed to create request: %v", err)), nil
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return core.NewErrorResult(fmt.Sprintf("Failed to execute webhook: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return core.NewSuccessResult("Evento enviado exitosamente al calendario"), nil
	}

	return core.NewErrorResult(fmt.Sprintf("Webhook returned status %d", resp.StatusCode)), nil
}

func (t *MakeCalendarTool) Spec() core.ToolSpec {
	return core.ToolSpec{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.ParametersSchema(),
	}
}
