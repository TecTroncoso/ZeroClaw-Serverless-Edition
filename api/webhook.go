// Package api provides the Vercel Serverless Function entrypoint.
// This file is automatically compiled by Vercel when placed in the /api folder.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zeroclaw/zeroclaw-go/pkg/agent"
	"github.com/zeroclaw/zeroclaw-go/pkg/channels"
	"github.com/zeroclaw/zeroclaw-go/pkg/core"
	"github.com/zeroclaw/zeroclaw-go/pkg/memory"
	"github.com/zeroclaw/zeroclaw-go/pkg/providers"
	"github.com/zeroclaw/zeroclaw-go/pkg/tools"
)

// ============================================================================
// INITIALIZATION (Cold Start)
// ============================================================================

var (
	// Global instances initialized on cold start
	mem     core.Memory
	prov    core.Provider
	toolz   []core.Tool
	ident   *core.IdentityConfig
)

func init() {
	log.Println("ZeroClaw: Initializing serverless function...")

	// Initialize Memory (Supabase)
	dbURL := os.Getenv("SUPABASE_DB_URL")
	if dbURL == "" {
		log.Println("ZeroClaw: WARNING - SUPABASE_DB_URL not set, memory disabled")
	} else {
		memBackend, err := memory.NewSupabaseMemory(&memory.SupabaseConfig{ConnectionString: dbURL})
		if err != nil {
			log.Printf("ZeroClaw: ERROR - Failed to initialize memory: %v", err)
			// Continue without memory - graceful degradation
			mem = nil
		} else if memBackend == nil {
			// NewSupabaseMemory returns nil when connection fails (graceful degradation)
			log.Println("ZeroClaw: WARNING - Memory backend unavailable, continuing without memory features")
			mem = nil
		} else {
			mem = memBackend
			log.Println("ZeroClaw: Memory initialized successfully")
		}
	}

	// Initialize Chat Provider
	prov = providers.NewOpenAIProvider()
	log.Printf("ZeroClaw: Chat Provider initialized (model: %s, base_url: %s)", os.Getenv("OPENAI_MODEL"), os.Getenv("OPENAI_BASE_URL"))

	// Initialize Embedding Provider
	var embProvider core.EmbeddingService
	embAPIKey := os.Getenv("EMBEDDING_API_KEY")
	if embAPIKey != "" {
		embModel := os.Getenv("EMBEDDING_MODEL")
		if embModel == "" {
			embModel = "text-embedding-3-small"
		}
		embConfig := &providers.OpenAIConfig{
			APIKey:  embAPIKey,
			BaseURL: os.Getenv("EMBEDDING_BASE_URL"),
			Model:   embModel,
		}
		embProvider = providers.NewOpenAIProviderWithConfig(embConfig)
		log.Printf("ZeroClaw: Embedding Provider initialized (model: %s, base_url: %s)", embModel, embConfig.BaseURL)
	} else {
		// Fallback to Chat Provider if embedding variables are not set
		if svc, ok := prov.(core.EmbeddingService); ok {
			embProvider = svc
			log.Println("ZeroClaw: Embedding Provider defaulting to Chat Provider")
		}
	}

	// Inject embedding service into memory backend for vector search
	if memBackend, ok := mem.(*memory.SupabaseMemory); ok && memBackend != nil {
		if embProvider != nil {
			memBackend.SetEmbeddingService(embProvider)
			log.Println("ZeroClaw: Embedding service injected into memory backend")
		}
	}

	// Initialize Tools
	toolz = []core.Tool{
		tools.NewWebSearchTool(),
		tools.NewWebFetchTool(),
		tools.NewHTTPRequestTool(),
	}
	log.Printf("ZeroClaw: %d tools registered", len(toolz))

	// Initialize Identity
	ident = core.LoadIdentityConfig()
	log.Println("ZeroClaw: Identity loaded")
}

// ============================================================================
// HTTP HANDLER
// ============================================================================

// handler is the main entrypoint for Vercel Serverless Functions.
func handler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
	defer cancel()

	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("ZeroClaw: ERROR - Failed to read body: %v", err)
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Determine channel from query parameter
	channelParam := r.URL.Query().Get("channel")
	if channelParam == "" {
		channelParam = detectChannelFromPath(r.URL.Path)
	}

	log.Printf("ZeroClaw: Received request via channel=%s, path=%s", channelParam, r.URL.Path)

	// Route to appropriate channel handler
	var incomingMsg *channels.IncomingMessage
	var responseChannel channels.Channel
	var responseRecipient string
	var platformResponse interface{}

	switch strings.ToLower(channelParam) {
	case "telegram":
		incomingMsg, responseChannel, responseRecipient, platformResponse = handleTelegram(body)
	case "discord":
		incomingMsg, responseChannel, responseRecipient, platformResponse = handleDiscord(body, w, r)
	case "slack":
		incomingMsg, responseChannel, responseRecipient, platformResponse = handleSlack(body, w)
	case "whatsapp":
		incomingMsg, responseChannel, responseRecipient, platformResponse = handleWhatsApp(body)
	default:
		// Try to auto-detect channel from payload structure
		incomingMsg, responseChannel, responseRecipient, platformResponse = autoDetectChannel(body, w, r)
	}

	// Handle platform-specific immediate response
	if platformResponse != nil {
		// Some platforms need immediate ACK before processing
		switch v := platformResponse.(type) {
		case *channels.DiscordInteractionResponse:
			respondJSON(w, v, http.StatusOK)
			// For Discord, we need to send followup after ACK
			// Flush the response so Vercel sends it to Discord immediately
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			if incomingMsg != nil {
				processAndRespond(ctx, incomingMsg, responseChannel, responseRecipient)
			}
			return
		case string:
			if v == "unauthorized" {
				return // Already wrote HTTP Error in the handler
			}
			// Plain text response (e.g., Slack challenge)
			respondText(w, v, http.StatusOK)
			return
		}
	}

	// If no message extracted, return error
	if incomingMsg == nil {
		log.Printf("ZeroClaw: No message extracted from request")
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	// Send "typing..." indicator asynchronously — the LLM processing takes
	// several seconds, giving this goroutine plenty of time to complete before
	// Vercel freezes the container after the HTTP response is sent.
	if responseChannel != nil && responseRecipient != "" {
		go func() {
			if err := responseChannel.SendTyping(ctx, responseRecipient); err != nil {
				log.Printf("Warning: typing indicator failed: %v", err)
			}
		}()
	}

	// Process message synchronously (Vercel freezes after response)
	response, err := processMessage(ctx, incomingMsg)
	if err != nil {
		log.Printf("ZeroClaw: ERROR - Processing failed: %v", err)
		if strings.Contains(err.Error(), "context deadline exceeded") {
			response = "⏳ Lo siento, mi cerebro tardó demasiado en procesar esto y se agotó el tiempo de espera. ¿Podrías intentar preguntarme de otra forma o más resumido?"
		} else {
			response = "⚠️ Ocurrió un error interno al conectar con mi red neuronal. Intenta en unos minutos."
		}
	}

	// Send response through the channel
	if responseChannel != nil && responseRecipient != "" {
		if err := responseChannel.Send(responseRecipient, response); err != nil {
			log.Printf("ZeroClaw: ERROR - Failed to send response: %v", err)
		}
	}

	// Log timing
	duration := time.Since(startTime)
	log.Printf("ZeroClaw: Request completed in %v", duration)

	// Return 200 OK to platform
	w.WriteHeader(http.StatusOK)
}

// ============================================================================
// CHANNEL HANDLERS
// ============================================================================

// handleTelegram processes a Telegram webhook request.
func handleTelegram(body []byte) (*channels.IncomingMessage, channels.Channel, string, interface{}) {
	update, err := channels.ParseTelegramWebhook(body)
	if err != nil {
		log.Printf("ZeroClaw: Failed to parse Telegram webhook: %v", err)
		return nil, nil, "", nil
	}

	// Extract message details
	text := update.ExtractMessage()
	if text == "" {
		return nil, nil, "", nil
	}

	chatID := update.ExtractChatID()
	senderID := update.ExtractSenderID()
	senderName := update.ExtractSenderName()

	// Log the sessionID (based on Telegram user ID) for diagnostic purposes
	log.Printf("ZeroClaw: Telegram sessionID=%s", senderID)

	msg := &channels.IncomingMessage{
		Channel:    "telegram",
		SenderID:   senderID,
		SenderName: senderName,
		ChatID:     chatID,
		Text:       text,
		Timestamp:  time.Now(),
		Metadata: map[string]string{
			"update_id": fmt.Sprintf("%d", update.UpdateID),
		},
	}

	ch := channels.NewTelegramChannel()
	return msg, ch, chatID, nil
}

// handleDiscord processes a Discord interaction.
func handleDiscord(body []byte, w http.ResponseWriter, r *http.Request) (*channels.IncomingMessage, channels.Channel, string, interface{}) {
	ch := channels.NewDiscordChannel()

	if r != nil && w != nil {
		signature := r.Header.Get("X-Signature-Ed25519")
		timestamp := r.Header.Get("X-Signature-Timestamp")
		if !ch.VerifySignature(signature, timestamp, body) {
			log.Printf("ZeroClaw: Invalid Discord signature")
			http.Error(w, "invalid request signature", http.StatusUnauthorized)
			return nil, nil, "", "unauthorized"
		}
	}

	interaction, err := channels.ParseDiscordInteraction(body)
	if err != nil {
		log.Printf("ZeroClaw: Failed to parse Discord interaction: %v", err)
		return nil, nil, "", nil
	}

	// Handle ping (health check)
	if interaction.IsPing() {
		return nil, nil, "", channels.NewPongResponse()
	}

	// Extract message details
	text := interaction.ExtractMessage()
	if text == "" {
		return nil, nil, "", channels.NewEphemeralResponse("I couldn't understand that command.")
	}

	channelID := interaction.ExtractChannelID()
	userID := interaction.ExtractUserID()
	userName := interaction.ExtractUserName()

	msg := &channels.IncomingMessage{
		Channel:    "discord",
		SenderID:   userID,
		SenderName: userName,
		ChatID:     channelID,
		Text:       text,
		Timestamp:  time.Now(),
		Metadata: map[string]string{
			"interaction_id":    interaction.ID,
			"interaction_token": interaction.Token,
			"application_id":    interaction.ApplicationID,
		},
	}

	// Return deferred response - we'll send followup after processing
	return msg, ch, channelID, channels.NewDeferredResponse()
}

// handleSlack processes a Slack event.
func handleSlack(body []byte, w http.ResponseWriter) (*channels.IncomingMessage, channels.Channel, string, interface{}) {
	event, err := channels.ParseSlackEvent(body)
	if err != nil {
		log.Printf("ZeroClaw: Failed to parse Slack event: %v", err)
		return nil, nil, "", nil
	}

	// Handle URL verification challenge
	if event.IsURLVerification() {
		return nil, nil, "", event.Challenge
	}

	// Skip bot messages
	if event.IsBotMessage() {
		return nil, nil, "", nil
	}

	// Extract message details
	text := event.ExtractMessage()
	if text == "" {
		return nil, nil, "", nil
	}

	channelID := event.ExtractChannelID()
	userID := event.ExtractUserID()

	msg := &channels.IncomingMessage{
		Channel:    "slack",
		SenderID:   userID,
		SenderName: "", // Slack doesn't always provide name in event
		ChatID:     channelID,
		Text:       text,
		Timestamp:  time.Now(),
		Metadata: map[string]string{
			"event_id":   event.EventID,
			"team_id":    event.TeamID,
			"thread_ts":  event.ExtractThreadTs(),
		},
	}

	ch := channels.NewSlackChannel()
	return msg, ch, channelID, nil
}

// handleWhatsApp processes a WhatsApp webhook.
func handleWhatsApp(body []byte) (*channels.IncomingMessage, channels.Channel, string, interface{}) {
	webhook, err := channels.ParseWhatsAppWebhook(body)
	if err != nil {
		log.Printf("ZeroClaw: Failed to parse WhatsApp webhook: %v", err)
		return nil, nil, "", nil
	}

	// Skip status updates
	if webhook.IsStatusUpdate() {
		return nil, nil, "", nil
	}

	// Extract first message
	msg, contact := webhook.ExtractFirstMessage()
	if msg == nil {
		return nil, nil, "", nil
	}

	text := msg.ExtractMessageText()
	if text == "" {
		return nil, nil, "", nil
	}

	senderPhone := msg.ExtractSenderPhone()
	senderName := ""
	if contact != nil {
		senderName = contact.ExtractSenderName()
	}

	incomingMsg := &channels.IncomingMessage{
		Channel:    "whatsapp",
		SenderID:   senderPhone,
		SenderName: senderName,
		ChatID:     senderPhone, // WhatsApp uses phone as chat ID
		Text:       text,
		Timestamp:  time.Now(),
		Metadata: map[string]string{
			"message_id":   msg.ID,
			"message_type": msg.Type,
		},
	}

	ch := channels.NewWhatsAppChannel()
	return incomingMsg, ch, senderPhone, nil
}

// autoDetectChannel attempts to detect the channel from payload structure.
func autoDetectChannel(body []byte, w http.ResponseWriter, r *http.Request) (*channels.IncomingMessage, channels.Channel, string, interface{}) {
	// Try Telegram
	if update, err := channels.ParseTelegramWebhook(body); err == nil && update.Message != nil {
		return handleTelegram(body)
	}

	// Try Discord
	if interaction, err := channels.ParseDiscordInteraction(body); err == nil {
		if interaction.IsPing() {
			return nil, nil, "", channels.NewPongResponse()
		}
		return handleDiscord(body, w, r)
	}

	// Try Slack
	if event, err := channels.ParseSlackEvent(body); err == nil {
		if event.IsURLVerification() {
			return nil, nil, "", event.Challenge
		}
		return handleSlack(body, nil)
	}

	// Try WhatsApp
	if webhook, err := channels.ParseWhatsAppWebhook(body); err == nil && webhook.HasMessages() {
		return handleWhatsApp(body)
	}

	return nil, nil, "", nil
}

// ============================================================================
// MESSAGE PROCESSING
// ============================================================================

// processMessage runs the agent loop to process an incoming message.
func processMessage(ctx context.Context, msg *channels.IncomingMessage) (string, error) {
	// Build agent configuration
	config := &agent.Config{
		MaxIterations: 5,
		Timeout:       50 * time.Second,
		Temperature:   0.7,
		Model:         os.Getenv("OPENAI_MODEL"),
		SessionID:     msg.SenderID,
	}

	// Inject session-specific memory tool if memory is available
	sessionTools := make([]core.Tool, len(toolz))
	copy(sessionTools, toolz)
	if mem != nil {
		sessionTools = append(sessionTools, tools.NewMemoryStoreTool(mem, &config.SessionID))
	}

	// Create agent
	ag := agent.NewAgent(mem, prov, sessionTools, config, ident)

	// Run agent loop
	result, err := ag.Run(ctx, msg.Text)
	if err != nil {
		return "", fmt.Errorf("agent execution failed: %w", err)
	}

	return result.Response, nil
}

// processAndRespond processes a message and sends response asynchronously.
// Used for platforms like Discord that require immediate ACK.
func processAndRespond(ctx context.Context, msg *channels.IncomingMessage, ch channels.Channel, recipient string) {
	response, err := processMessage(ctx, msg)
	if err != nil {
		log.Printf("ZeroClaw: ERROR - Async processing failed: %v", err)
		if strings.Contains(err.Error(), "context deadline exceeded") {
			response = "⏳ Lo siento, mi cerebro tardó demasiado en procesar esto y se agotó el tiempo de espera. ¿Podrías intentar preguntarme de otra forma o más resumido?"
		} else {
			response = "⚠️ Ocurrió un error interno al conectar con mi red neuronal. Intenta en unos minutos."
		}
	}

	if ch != nil && recipient != "" {
		if err := ch.Send(recipient, response); err != nil {
			log.Printf("ZeroClaw: ERROR - Failed to send async response: %v", err)
		}
	}
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// detectChannelFromPath attempts to detect channel from URL path.
func detectChannelFromPath(path string) string {
	path = strings.ToLower(path)
	if strings.Contains(path, "telegram") {
		return "telegram"
	}
	if strings.Contains(path, "discord") {
		return "discord"
	}
	if strings.Contains(path, "slack") {
		return "slack"
	}
	if strings.Contains(path, "whatsapp") {
		return "whatsapp"
	}
	return ""
}

// respondJSON sends a JSON response.
func respondJSON(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondText sends a plain text response.
func respondText(w http.ResponseWriter, text string, status int) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(status)
	w.Write([]byte(text))
}

// ============================================================================
// VERCEL ENTRYPOINT
// ============================================================================

// Vercel expects a function named exactly like the file (without extension).
// For api/webhook.go, Vercel looks for a function named "Webhook" or a Handler.

// Handler is the main entrypoint for Vercel Serverless Functions.
func Handler(w http.ResponseWriter, r *http.Request) {
	handler(w, r)
}

// Webhook is the Vercel entrypoint (alternative naming).
func Webhook(w http.ResponseWriter, r *http.Request) {
	handler(w, r)
}
