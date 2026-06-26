package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/endru/kiw-test/internal/db"
	"github.com/endru/kiw-test/internal/whatsapp"
)

// WhatsAppSender is the interface for sending WhatsApp messages.
// It is defined in the consumer (webhook) package for dependency inversion.
type WhatsAppSender interface {
	SendTextMessage(to, text string) error
	SendButtonMessage(to, text string, buttons []whatsapp.Button) error
}

// Handler processes WhatsApp webhook callbacks.
type Handler struct {
	verifyToken string
	sender      WhatsAppSender
	dbStore     db.Store
}

// NewHandler creates a new webhook handler.
func NewHandler(verifyToken string, sender WhatsAppSender, dbStore db.Store) *Handler {
	return &Handler{
		verifyToken: verifyToken,
		sender:      sender,
		dbStore:     dbStore,
	}
}

// HandleVerification handles the webhook verification GET request from Meta.
// Meta sends: GET /webhook?hub.mode=subscribe&hub.verify_token=TOKEN&hub.challenge=CHALLENGE
// The server must respond with the challenge string.
func (h *Handler) HandleVerification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
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

// HandleEvent processes incoming webhook events from Meta (POST).
func (h *Handler) HandleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	if payload.Object != "whatsapp_business_account" {
		slog.Warn("unexpected webhook object type", "object", payload.Object)
		http.Error(w, "unexpected object type", http.StatusBadRequest)
		return
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			value := change.Value

			// Process status updates (sent, delivered, read)
			for _, status := range value.Statuses {
				slog.Info("message status update",
					"message_id", status.ID,
					"status", status.Status,
					"recipient_id", status.RecipientID,
				)
			}

			// Process incoming messages
			for _, msg := range value.Messages {
				senderName := "Unknown"
				for _, contact := range value.Contacts {
					if contact.WAID == msg.From {
						senderName = contact.Profile.Name
						break
					}
				}
				h.processMessage(r.Context(), msg, senderName)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// processMessage handles a single incoming message, queries/upserts contacts and sessions, and logs/replies.
func (h *Handler) processMessage(ctx context.Context, msg Message, senderName string) {
	slog.Info("received message",
		"from_name", senderName,
		"from_phone", msg.From,
		"type", msg.Type,
		"message_id", msg.ID,
	)

	// 1. Get or create Contact
	contact, err := h.dbStore.GetOrCreateContact(ctx, msg.From, senderName)
	if err != nil {
		slog.Error("failed to get or create contact", "phone", msg.From, "error", err)
	} else {
		senderName = contact.Name
	}

	// 2. Get active session or create new one
	session, err := h.dbStore.GetActiveSession(ctx, msg.From)
	if err != nil {
		slog.Error("failed to get active session", "phone", msg.From, "error", err)
	}
	if session == nil {
		session, err = h.dbStore.CreateSession(ctx, msg.From, "bot", "open")
		if err != nil {
			slog.Error("failed to create new session", "phone", msg.From, "error", err)
			return // Return early since we need a session to track messages properly
		}
		slog.Info("created new chat session for customer", "phone", msg.From, "session_id", session.ID)
	}

	// Extract message body
	var body string
	if msg.Type == "text" && msg.Text != nil {
		body = msg.Text.Body
	} else {
		body = fmt.Sprintf("[%s message]", msg.Type)
	}

	// 3. Log incoming message
	if err := h.dbStore.LogMessage(ctx, session.ID, msg.From, "system", body); err != nil {
		slog.Error("failed to log incoming message", "session_id", session.ID, "error", err)
	}

	// 4. If current handler is human, skip auto-response (bot reply)
	if session.CurrentHandler == "human" {
		slog.Info("skipping auto-reply: session is handled by human", "phone", msg.From, "session_id", session.ID)
		return
	}

	// 5. Bot auto-reply logic
	switch msg.Type {
	case "text":
		if msg.Text == nil {
			slog.Warn("text message with nil text content", "message_id", msg.ID)
			return
		}

		if msg.From == "6282135364500" {
			reply := "Hello Koh Endru, what can I do for you?"
			buttons := []whatsapp.Button{
				{ID: "btn_help", Title: "Help"},
				{ID: "btn_status", Title: "Check Status"},
			}
			if err := h.sender.SendButtonMessage(msg.From, reply, buttons); err != nil {
				slog.Error("failed to send button message",
					"to", msg.From,
					"message_id", msg.ID,
					"error", err,
				)
			} else {
				// Log outgoing bot response
				if err := h.dbStore.LogMessage(ctx, session.ID, "bot", msg.From, reply); err != nil {
					slog.Error("failed to log outgoing button message", "session_id", session.ID, "error", err)
				}
			}
		} else {
			reply := "Sorry, auto reply message currently not available since this still on development"
			if err := h.sender.SendTextMessage(msg.From, reply); err != nil {
				slog.Error("failed to send text message",
					"to", msg.From,
					"message_id", msg.ID,
					"error", err,
				)
			} else {
				// Log outgoing bot response
				if err := h.dbStore.LogMessage(ctx, session.ID, "bot", msg.From, reply); err != nil {
					slog.Error("failed to log outgoing text message", "session_id", session.ID, "error", err)
				}
			}
		}

	default:
		slog.Info("unhandled message type",
			"type", msg.Type,
			"from", msg.From,
		)
	}
}
