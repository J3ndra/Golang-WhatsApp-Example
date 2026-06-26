// Package webhook handles Meta's WhatsApp Business Cloud API webhooks.
//
// Two HTTP endpoints are exposed:
//
//	GET  /webhook  – Meta's one-time verification handshake
//	POST /webhook  – Incoming message events (text, interactive, status updates)
//
// Message processing follows a four-step pipeline:
//
//	Step A: Resolve the sender's name from the Contacts table (default "Customer").
//	Step B: Fetch or create an open ChatSession for the phone number.
//	Step C: Bot-mode logic  – auto-reply to greetings, escalate on trigger phrases.
//	Step D: Human-mode logic – skip the bot entirely; broadcast to the CS panel via WebSocket.
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/endru/kiw-test/internal/db"
	"github.com/endru/kiw-test/internal/whatsapp"
	"github.com/endru/kiw-test/internal/ws"
)

// ─── Greeting & Escalation Sets ────────────────────────────────────────────

// greetings is the set of message texts that trigger the bot's welcome reply.
// Comparison is case-insensitive and whitespace-trimmed.
var greetings = map[string]struct{}{
	"hello":    {},
	"hai":      {},
	"hi":       {},
	"halo":     {},
	"hey":      {},
	"hei":      {},
	"good day": {},
	"howdy":    {},
}

// escalationPhrases trigger handoff from bot to a human CS agent.
// Also triggered when the user taps a button with one of these IDs/titles.
var escalationPhrases = map[string]struct{}{
	"ask other question": {},
	"talk to human":      {},
	"human agent":        {},
	"agent":              {},
	"cs":                 {},
}

// escalationButtonIDs are button IDs (set at send-time) that escalate to human.
var escalationButtonIDs = map[string]struct{}{
	"btn_ask_other": {},
	"btn_human":     {},
}

// ─── Interfaces ─────────────────────────────────────────────────────────────

// WhatsAppSender is the interface for sending WhatsApp messages.
// Defined here (consumer side) for dependency inversion / testability.
type WhatsAppSender interface {
	SendTextMessage(to, text string) error
	SendButtonMessage(to, text string, buttons []whatsapp.Button) error
}

// ─── Handler ────────────────────────────────────────────────────────────────

// Handler processes WhatsApp webhook callbacks.
type Handler struct {
	verifyToken string
	sender      WhatsAppSender
	dbStore     db.Store
	hub         *ws.Hub // WebSocket hub for broadcasting to the CS panel
}

// NewHandler creates a new webhook handler with all dependencies injected.
func NewHandler(verifyToken string, sender WhatsAppSender, dbStore db.Store, hub *ws.Hub) *Handler {
	return &Handler{
		verifyToken: verifyToken,
		sender:      sender,
		dbStore:     dbStore,
		hub:         hub,
	}
}

// ─── GET /webhook ───────────────────────────────────────────────────────────

// HandleVerification handles the webhook verification GET request from Meta.
//
// Meta sends:
//
//	GET /webhook?hub.mode=subscribe&hub.verify_token=TOKEN&hub.challenge=CHALLENGE
//
// The server must echo back the challenge string with HTTP 200 to confirm ownership.
func (h *Handler) HandleVerification(w http.ResponseWriter, r *http.Request) {
	mode      := r.URL.Query().Get("hub.mode")
	token     := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	slog.Info("webhook verification attempt",
		"mode", mode,
		"has_token", token != "",
		"has_challenge", challenge != "",
	)

	if mode != "subscribe" {
		slog.Warn("webhook verification: wrong mode", "mode", mode)
		http.Error(w, "invalid hub.mode", http.StatusBadRequest)
		return
	}

	if token != h.verifyToken {
		slog.Warn("webhook verification: token mismatch")
		http.Error(w, "invalid verify_token", http.StatusForbidden)
		return
	}

	if challenge == "" {
		slog.Warn("webhook verification: missing challenge")
		http.Error(w, "missing hub.challenge", http.StatusBadRequest)
		return
	}

	slog.Info("webhook verification successful")
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(challenge))
}

// ─── POST /webhook ──────────────────────────────────────────────────────────

// HandleEvent processes incoming POST webhook events from Meta.
// Meta always expects HTTP 200 quickly, so the processing per message is
// synchronous but bounded by per-call context timeouts (inherited from request).
func (h *Handler) HandleEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read request body", "error", err)
		http.Error(w, "failed to read body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Error("failed to parse webhook payload", "error", err)
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Meta only sends "whatsapp_business_account" for WA events.
	if payload.Object != "whatsapp_business_account" {
		slog.Warn("unexpected webhook object type", "object", payload.Object)
		http.Error(w, "unexpected object type", http.StatusBadRequest)
		return
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			value := change.Value

			// Log delivery/read status updates — no further action required.
			for _, status := range value.Statuses {
				slog.Info("message status update",
					"message_id", status.ID,
					"status", status.Status,
					"recipient_id", status.RecipientID,
				)
			}

			// Process each incoming message.
			for _, msg := range value.Messages {
				// Resolve the sender name from the WA profile in the webhook payload.
				// This is the name that WhatsApp itself provides; we use it as the
				// fallback when the contact is not yet in our database.
				senderName := "Customer"
				for _, c := range value.Contacts {
					if c.WAID == msg.From && c.Profile.Name != "" {
						senderName = c.Profile.Name
						break
					}
				}
				h.processMessage(r.Context(), msg, senderName)
			}
		}
	}

	// Meta requires a 200 OK reply quickly; any other code triggers retries.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// ─── Core message pipeline ──────────────────────────────────────────────────

// processMessage runs the full Step A → D pipeline for a single incoming message.
func (h *Handler) processMessage(ctx context.Context, msg Message, whatsappName string) {
	slog.Info("received message",
		"from", msg.From,
		"type", msg.Type,
		"message_id", msg.ID,
	)

	// ── Step A: Resolve contact name ────────────────────────────────────────
	// Look up the Contacts table.  If the number is known, use the stored name.
	// If not, fall back to the name Meta provided in the webhook payload, or
	// "Customer" if even that is empty.
	contact, err := h.dbStore.GetOrCreateContact(ctx, msg.From, whatsappName)
	customerName := whatsappName
	if err != nil {
		slog.Error("step A: failed to get/create contact",
			"phone", msg.From, "error", err)
	} else {
		// Prefer the database-stored name (may have been set by CS staff).
		if contact.Name != "" {
			customerName = contact.Name
		}
	}

	slog.Info("step A: contact resolved", "phone", msg.From, "name", customerName)

	// ── Step B: Get or create an open ChatSession ────────────────────────────
	session, err := h.dbStore.GetActiveSession(ctx, msg.From)
	if err != nil {
		slog.Error("step B: failed to query active session",
			"phone", msg.From, "error", err)
	}
	if session == nil {
		// No open session yet — start a new one with the bot as handler.
		session, err = h.dbStore.CreateSession(ctx, msg.From, "bot", "open")
		if err != nil {
			slog.Error("step B: failed to create session",
				"phone", msg.From, "error", err)
			return // cannot proceed without a session
		}
		slog.Info("step B: created new session",
			"phone", msg.From, "session_id", session.ID)
	} else {
		slog.Info("step B: found existing session",
			"phone", msg.From,
			"session_id", session.ID,
			"handler", session.CurrentHandler,
		)
	}

	// Extract the plain-text body regardless of message type for logging.
	body := extractBody(msg)

	// Log every incoming message to the Messages table.
	if err := h.dbStore.LogMessage(ctx, session.ID, msg.From, "system", body); err != nil {
		slog.Error("failed to log incoming message",
			"session_id", session.ID, "error", err)
	}

	// ── Step D: Human handler — bypass bot entirely ──────────────────────────
	if session.CurrentHandler == "human" {
		h.handleHumanMode(ctx, msg, session, customerName, body)
		return
	}

	// ── Step C: Bot handler ──────────────────────────────────────────────────
	h.handleBotMode(ctx, msg, session, customerName, body)
}

// ─── Step C – Bot mode ──────────────────────────────────────────────────────

// handleBotMode implements the bot auto-reply logic.
//
// Triggers:
//   - Greeting text  → reply "Hi [Name], how can I help you?" + escalation buttons
//   - Escalation text or button → update handler to "human", emit WS event
func (h *Handler) handleBotMode(ctx context.Context, msg Message, session *db.ChatSession, name, body string) {
	slog.Info("step C: bot mode processing",
		"phone", msg.From, "type", msg.Type, "body", body)

	switch msg.Type {

	case "text":
		normalized := strings.ToLower(strings.TrimSpace(body))

		// Check escalation first — user typed something like "ask other question".
		if _, isEscalation := escalationPhrases[normalized]; isEscalation {
			h.escalateToHuman(ctx, msg.From, session, name, body)
			return
		}

		// Check if the message is a greeting.
		if _, isGreeting := greetings[normalized]; isGreeting {
			h.sendGreetingReply(msg.From, session, name, body)
			return
		}

		// Any other text while bot is handling — send a generic holding reply.
		reply := fmt.Sprintf(
			"Hi %s! 👋 I'm your virtual assistant. "+
				"Type *Hello* to start, or tap *Ask other question* "+
				"to speak with a human agent.", name)
		if err := h.sender.SendTextMessage(msg.From, reply); err != nil {
			slog.Error("step C: failed to send generic bot reply",
				"phone", msg.From, "error", err)
		} else {
			h.dbStore.LogMessage(ctx, session.ID, "bot", msg.From, reply)
		}

	case "interactive":
		// User tapped a quick-reply button.
		if msg.Interactive == nil {
			slog.Warn("step C: interactive message with nil content", "id", msg.ID)
			return
		}
		buttonID    := msg.Interactive.ButtonReply.ID
		buttonTitle := msg.Interactive.ButtonReply.Title

		slog.Info("step C: button tapped",
			"phone", msg.From,
			"button_id", buttonID,
			"button_title", buttonTitle,
		)

		// Check escalation button IDs or titles.
		_, idMatch    := escalationButtonIDs[buttonID]
		_, titleMatch := escalationPhrases[strings.ToLower(strings.TrimSpace(buttonTitle))]

		if idMatch || titleMatch {
			h.escalateToHuman(ctx, msg.From, session, name, buttonTitle)
			return
		}

		// Non-escalation button — log and acknowledge.
		slog.Info("step C: non-escalation button, no action taken",
			"button_id", buttonID)

	default:
		slog.Info("step C: unhandled message type",
			"type", msg.Type, "from", msg.From)
	}
}

// sendGreetingReply sends "Hi [Name], how can I help you?" plus two quick-reply
// buttons: one for help and one to escalate to a human agent.
func (h *Handler) sendGreetingReply(phone string, session *db.ChatSession, name, originalBody string) {
	replyText := fmt.Sprintf("Hi %s! 👋 How can I help you today?", name)

	buttons := []whatsapp.Button{
		{ID: "btn_faq",       Title: "View FAQ"},
		{ID: "btn_ask_other", Title: "Ask other question"},
	}

	if err := h.sender.SendButtonMessage(phone, replyText, buttons); err != nil {
		slog.Error("step C: failed to send greeting reply",
			"phone", phone, "error", err)
		return
	}

	// Log the bot's outgoing message.
	// We use a background context clone so DB logging doesn't fail if the
	// HTTP request context is already cancelled.
	ctx := context.Background()
	h.dbStore.LogMessage(ctx, session.ID, "bot", phone, replyText)

	slog.Info("step C: greeting reply sent", "phone", phone, "name", name)
}

// escalateToHuman updates the session handler to "human" in the database and
// emits a WebSocket event to alert the CS panel.
func (h *Handler) escalateToHuman(
	ctx context.Context,
	phone string,
	session *db.ChatSession,
	name, triggerBody string,
) {
	slog.Info("step C: escalating session to human agent",
		"phone", phone, "session_id", session.ID)

	// Update the session handler in the database.
	if err := h.dbStore.UpdateSessionHandler(ctx, session.ID, "human"); err != nil {
		slog.Error("step C: failed to update session handler",
			"session_id", session.ID, "error", err)
		// Continue anyway — still emit the WS event so the CS agent sees it.
	}

	// Notify the customer that their request is being passed to a human.
	ackMsg := fmt.Sprintf(
		"Got it, %s! 🙌 I'm connecting you to a human agent. "+
			"Please wait a moment.", name)
	if err := h.sender.SendTextMessage(phone, ackMsg); err != nil {
		slog.Error("step C: failed to send escalation ack",
			"phone", phone, "error", err)
	} else {
		h.dbStore.LogMessage(ctx, session.ID, "bot", phone, ackMsg)
	}

	// ── Emit WebSocket event to the CS panel ─────────────────────────────────
	// EventType "escalated" tells the frontend to surface this conversation.
	h.hub.Broadcast(ws.WSEvent{
		EventType: "escalated",
		Phone:     phone,
		Name:      name,
		SessionID: session.ID,
		Message:   triggerBody,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})

	slog.Info("step C: ws escalation event broadcast",
		"phone", phone, "session_id", session.ID)
}

// ─── Step D – Human mode ────────────────────────────────────────────────────

// handleHumanMode bypasses the bot and broadcasts the message directly to the
// CS panel via WebSocket so a human agent can reply through the dashboard.
func (h *Handler) handleHumanMode(
	ctx context.Context,
	msg Message,
	session *db.ChatSession,
	name, body string,
) {
	slog.Info("step D: human mode — broadcasting to CS panel",
		"phone", msg.From,
		"session_id", session.ID,
		"body", body,
	)

	// Broadcast the raw message to every connected CS panel client.
	h.hub.Broadcast(ws.WSEvent{
		EventType: "new_message",
		Phone:     msg.From,
		Name:      name,
		SessionID: session.ID,
		Message:   body,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// extractBody returns the plain-text representation of a message.
// For text messages it returns the body; for interactive (button taps) it
// returns the button title; for all other types it returns a bracketed label.
func extractBody(msg Message) string {
	switch msg.Type {
	case "text":
		if msg.Text != nil {
			return msg.Text.Body
		}
	case "interactive":
		if msg.Interactive != nil {
			return msg.Interactive.ButtonReply.Title
		}
	}
	return fmt.Sprintf("[%s message]", msg.Type)
}
