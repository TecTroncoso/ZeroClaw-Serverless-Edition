// Package api provides the Vercel Serverless Function entrypoint.
// This file is automatically compiled by Vercel when placed in the /api folder.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
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
	
	// Si es una petición en segundo plano, desligamos el context de la conexión HTTP
	// para que no se cancele cuando la instancia enviadora corte la conexión.
	baseCtx := context.Background()
	if r.URL.Query().Get("bg") != "true" {
		baseCtx = r.Context()
	}
	ctx, cancel := context.WithTimeout(baseCtx, 55*time.Second)
	defer cancel()

	// Intercept setup route
	if r.URL.Query().Get("setup_discord") == "true" {
		setupDiscordCommand(w, r)
		return
	}

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

	// Create stream callback for progressive text generation
	var streamCallback core.StreamCallback
	var msgIDForTelegram int
	var isDiscordStreaming bool
	var updatesCount int

	if responseChannel != nil && responseRecipient != "" {
		if chName := responseChannel.Name(); chName == "telegram" {
			tc := responseChannel.(*channels.TelegramChannel)
			// Send initial message
			msgID, err := tc.SendWithID(responseRecipient, "Escorpion Assistant está pensando...")
			if err == nil {
				msgIDForTelegram = msgID
				var mu sync.Mutex
				var currentText string
				var lastEdit time.Time

				streamCallback = func(chunk string) {
					mu.Lock()
					defer mu.Unlock()
					currentText += chunk
					
					threshold := time.Duration(1500+(updatesCount*500)) * time.Millisecond
					if threshold > 4000*time.Millisecond {
						threshold = 4000 * time.Millisecond
					}

					if time.Since(lastEdit) >= threshold && strings.TrimSpace(currentText) != "" {
						tc.EditMessage(responseRecipient, msgID, currentText+" ✍️")
						lastEdit = time.Now()
						updatesCount++
					}
				}
			} else {
				log.Printf("ZeroClaw: Warning - failed to send initial telegram message: %v", err)
			}
		} else if chName == "discord" {
			dc := responseChannel.(*channels.DiscordChannel)
			parts := strings.SplitN(responseRecipient, "|", 2)
			if len(parts) == 2 {
				isDiscordStreaming = true
				token := parts[0]
				appID := parts[1]
				var mu sync.Mutex
				var currentText string
				var lastEdit time.Time

				streamCallback = func(chunk string) {
					mu.Lock()
					defer mu.Unlock()
					currentText += chunk
					
					threshold := time.Duration(1500+(updatesCount*500)) * time.Millisecond
					if threshold > 4000*time.Millisecond {
						threshold = 4000 * time.Millisecond
					}

					if time.Since(lastEdit) >= threshold && strings.TrimSpace(currentText) != "" {
						dc.EditInteractionResponse(appID, token, currentText+" ✍️")
						lastEdit = time.Now()
						updatesCount++
					}
				}
			}
		}

		// Keep typing indicator for other channels or as fallback
		go func() {
			if err := responseChannel.SendTyping(ctx, responseRecipient); err != nil {
				log.Printf("Warning: typing indicator failed: %v", err)
			}
		}()
	}

	// Process message synchronously (Vercel freezes after response)
	response, ag, err := processMessage(ctx, incomingMsg, streamCallback)
	if err != nil {
		log.Printf("ZeroClaw: ERROR - Processing failed: %v", err)
		if strings.Contains(err.Error(), "context deadline exceeded") {
			response = "⏳ Lo siento, mi cerebro tardó demasiado en procesar esto y se agotó el tiempo de espera. ¿Podrías intentar preguntarme de otra forma o más resumido?"
		} else {
			response = "⚠️ Ocurrió un error interno al conectar con mi red neuronal. Intenta en unos minutos."
		}
	}

	// Send final response through the channel
	if responseChannel != nil && responseRecipient != "" {
		var sendErr error
		if msgIDForTelegram != 0 {
			tc := responseChannel.(*channels.TelegramChannel)
			sendErr = tc.EditMessage(responseRecipient, msgIDForTelegram, response)
		} else if isDiscordStreaming {
			// Discord Send handles EditInteractionResponse based on recipient token|appID
			sendErr = responseChannel.Send(responseRecipient, response)
		} else {
			sendErr = responseChannel.Send(responseRecipient, response)
		}

		if sendErr != nil {
			log.Printf("ZeroClaw: ERROR - Failed to send final response: %v", sendErr)
		}
	}

	// Log timing
	duration := time.Since(startTime)
	log.Printf("ZeroClaw: Request completed in %v", duration)

	// Store conversation in DB *after* sending response to user to reduce perceived latency
	if ag != nil && len(incomingMsg.Text) >= 1 {
		ag.StoreConversation(incomingMsg.Text, response)
	}

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

	// Pattern Background Worker para Vercel:
	// Discord exige respuesta en 3s. OpenAI tarda +5s.
	// 1. Recibimos interacción principal (bg != true)
	// 2. Disparamos background request a nuestro propio Vercel y respondemos 200 OK a Discord (Pensando...)
	// 3. El background worker (bg == true) recibe la request, procesa con la IA, y envía PATCH para editar el mensaje.
	isBg := r != nil && r.URL.Query().Get("bg") == "true"

	if !isBg && r != nil {
		log.Printf("ZeroClaw: Firing background worker for Discord Interaction")
		triggerDiscordBackgroundWorker(r, body)
		return nil, nil, "", channels.NewDeferredResponse()
	}

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

	// Usamos token|app_id como recipient para que DiscordChannel sepa que debe hacer PATCH
	recipient := interaction.Token + "|" + interaction.ApplicationID

	// Devolvemos platformResponse=nil porque el worker completará la tarea normal y no responderá instantáneamente HTTP.
	return msg, ch, recipient, nil
}

// triggerDiscordBackgroundWorker envía una petición a la propia URL de Vercel para 
// procesar la interacción de forma asíncrona sin bloquear la respuesta de 3s obligatoria de Discord.
func triggerDiscordBackgroundWorker(r *http.Request, body []byte) {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	
	url := fmt.Sprintf("%s://%s/api/webhook?channel=discord&bg=true", scheme, r.Host)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-Ed25519", r.Header.Get("X-Signature-Ed25519"))
	req.Header.Set("X-Signature-Timestamp", r.Header.Get("X-Signature-Timestamp"))

	// Damos 1 segundo para que la petición salga. Aunque el cliente corte (y dé timeout), 
	// el servidor destino ya habrá comenzado gracias al context.Background()
	client := &http.Client{Timeout: 1 * time.Second}
	client.Do(req)
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
func processMessage(ctx context.Context, msg *channels.IncomingMessage, streamCallback core.StreamCallback) (string, *agent.Agent, error) {
	sessionID := os.Getenv("MASTER_SESSION_ID")
	if sessionID == "" {
		sessionID = "global_master_memory"
	}

	// Build agent configuration
	config := &agent.Config{
		MaxIterations: 5,
		Timeout:       50 * time.Second,
		Temperature:   0.7,
		Model:         os.Getenv("OPENAI_MODEL"),
		SessionID:     sessionID,
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
	result, err := ag.Run(ctx, msg.Text, streamCallback)
	if err != nil {
		return "", ag, fmt.Errorf("agent execution failed: %w", err)
	}

	return result.Response, ag, nil
}

// processAndRespond processes a message and sends response asynchronously.
// Used for platforms like Discord that require immediate ACK.
func processAndRespond(ctx context.Context, msg *channels.IncomingMessage, ch channels.Channel, recipient string) {
	response, ag, err := processMessage(ctx, msg, nil) // Fallback processing
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

	// Store in DB after sending!
	if ag != nil && len(msg.Text) >= 1 {
		ag.StoreConversation(msg.Text, response)
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

// ============================================================================
// DISCORD SETUP
// ============================================================================

// setupDiscordCommand registers the /chat command in Discord automatically.
func setupDiscordCommand(w http.ResponseWriter, r *http.Request) {
	appID := os.Getenv("DISCORD_APP_ID")
	token := os.Getenv("DISCORD_BOT_TOKEN")

	if appID == "" || token == "" {
		http.Error(w, "Error: Válido solo si configuras DISCORD_APP_ID y DISCORD_BOT_TOKEN en Vercel.", http.StatusBadRequest)
		return
	}

	payload := map[string]interface{}{
		"name":        "chat",
		"description": "Habla con la Inteligencia Artificial",
		"options": []map[string]interface{}{
			{
				"name":        "mensaje",
				"description": "El mensaje para enviarle a la IA",
				"type":        3, // String type
				"required":    true,
			},
		},
		"contexts":          []int{0, 1, 2}, // Guild, Bot DM, Private Channel
		"integration_types": []int{0, 1},    // Guild Install, User Install
	}

	b, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://discord.com/api/v10/applications/%s/commands", appID)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error conectando con Discord: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("✅ ¡Éxito! Comando /chat registrado correctamente en servidores y DMs.\nYa puedes cerrar esta pestaña y escribir /chat a tu bot en Discord."))
	} else {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("❌ Error registrando comando de Discord (Status %d): %s\n", resp.StatusCode, string(body))))
	}
}
