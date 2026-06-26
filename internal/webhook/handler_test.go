package webhook

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/endru/kiw-test/internal/db"
	"github.com/endru/kiw-test/internal/whatsapp"
	"github.com/endru/kiw-test/internal/ws"
)

// ─── Mock Store ─────────────────────────────────────────────────────────────

type mockStore struct {
	contacts map[string]*db.Contact
	sessions map[string]*db.ChatSession
	messages []db.Message
}

func newMockStore() *mockStore {
	return &mockStore{
		contacts: make(map[string]*db.Contact),
		sessions: make(map[string]*db.ChatSession),
	}
}

func (m *mockStore) GetOrCreateContact(ctx context.Context, phoneNumber, name string) (*db.Contact, error) {
	if c, ok := m.contacts[phoneNumber]; ok {
		return c, nil // return existing stored name (do NOT overwrite with WA name)
	}
	c := &db.Contact{PhoneNumber: phoneNumber, Name: name}
	m.contacts[phoneNumber] = c
	return c, nil
}

func (m *mockStore) GetActiveSession(ctx context.Context, customerPhoneNumber string) (*db.ChatSession, error) {
	for _, s := range m.sessions {
		if s.CustomerPhoneNumber == customerPhoneNumber && s.SessionStatus == "open" {
			return s, nil
		}
	}
	return nil, nil
}

func (m *mockStore) CreateSession(ctx context.Context, customerPhoneNumber string, currentHandler, status string) (*db.ChatSession, error) {
	s := &db.ChatSession{
		ID:                  "mock-session-id",
		CustomerPhoneNumber: customerPhoneNumber,
		CurrentHandler:      currentHandler,
		SessionStatus:       status,
	}
	m.sessions[s.ID] = s
	return s, nil
}

func (m *mockStore) UpdateSessionHandler(ctx context.Context, sessionID string, handler string) error {
	if s, ok := m.sessions[sessionID]; ok {
		s.CurrentHandler = handler
		return nil
	}
	return fmt.Errorf("session not found")
}

func (m *mockStore) UpdateSessionStatus(ctx context.Context, sessionID string, status string) error {
	if s, ok := m.sessions[sessionID]; ok {
		s.SessionStatus = status
		return nil
	}
	return fmt.Errorf("session not found")
}

func (m *mockStore) LogMessage(ctx context.Context, sessionID string, sender string, recipient string, body string) error {
	m.messages = append(m.messages, db.Message{
		SessionID:            sessionID,
		SenderPhoneNumber:    sender,
		RecipientPhoneNumber: recipient,
		Body:                 body,
	})
	return nil
}

func (m *mockStore) Close() error { return nil }

// ─── Mock Sender ─────────────────────────────────────────────────────────────

type mockSender struct {
	messages []sentMessage
	err      error
}

type sentMessage struct {
	to      string
	text    string
	isButton bool
}

func (m *mockSender) SendTextMessage(to, text string) error {
	m.messages = append(m.messages, sentMessage{to: to, text: text})
	return m.err
}

func (m *mockSender) SendButtonMessage(to, text string, buttons []whatsapp.Button) error {
	m.messages = append(m.messages, sentMessage{to: to, text: text, isButton: true})
	return m.err
}

// ─── Helper ───────────────────────────────────────────────────────────────────

// newTestHandler wires up a Handler with mock dependencies and a no-op ws.Hub.
func newTestHandler(verifyToken string, sender WhatsAppSender) *Handler {
	return NewHandler(verifyToken, sender, newMockStore(), ws.NewHub())
}

// newTestHandlerWithStore wires up a Handler with a pre-populated store.
func newTestHandlerWithStore(verifyToken string, sender WhatsAppSender, store *mockStore) *Handler {
	return NewHandler(verifyToken, sender, store, ws.NewHub())
}

// webhookBody builds a minimal WhatsApp webhook POST body for a text message.
func webhookBody(from, waName, msgText string) string {
	return fmt.Sprintf(`{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "123",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {"display_phone_number": "15551234567", "phone_number_id": "123456"},
					"contacts": [{"profile": {"name": %q}, "wa_id": %q}],
					"messages": [{
						"from": %q,
						"id": "wamid.abc123",
						"timestamp": "1234567890",
						"type": "text",
						"text": {"body": %q}
					}]
				}
			}]
		}]
	}`, waName, from, from, msgText)
}

// ─── Verification Tests ───────────────────────────────────────────────────────

func TestHandleVerification(t *testing.T) {
	handler := newTestHandler("valid-token", &mockSender{})

	tests := []struct {
		name           string
		query          string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "valid verification",
			query:          "hub.mode=subscribe&hub.verify_token=valid-token&hub.challenge=abc123",
			expectedStatus: http.StatusOK,
			expectedBody:   "abc123",
		},
		{
			name:           "wrong mode",
			query:          "hub.mode=unsubscribe&hub.verify_token=valid-token&hub.challenge=abc123",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "wrong token",
			query:          "hub.mode=subscribe&hub.verify_token=wrong-token&hub.challenge=abc123",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "missing challenge",
			query:          "hub.mode=subscribe&hub.verify_token=valid-token",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing all parameters",
			query:          "",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/webhook?"+tt.query, nil)
			rec := httptest.NewRecorder()

			handler.HandleVerification(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
			if tt.expectedBody != "" {
				body := strings.TrimSpace(rec.Body.String())
				if body != tt.expectedBody {
					t.Errorf("expected body %q, got %q", tt.expectedBody, body)
				}
			}
		})
	}
}

// TestHandleVerificationMethodNotAllowed verifies that a POST to HandleVerification
// returns 400 Bad Request (wrong hub.mode) when called directly — in production the
// mux pattern "GET /webhook" enforces the method constraint before the handler runs.
func TestHandleVerificationMethodNotAllowed(t *testing.T) {
	handler := newTestHandler("valid-token", &mockSender{})

	// Calling HandleVerification directly with POST bypasses the mux; the handler
	// sees empty query params and returns 400 for the wrong hub.mode.
	req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
	rec := httptest.NewRecorder()

	handler.HandleVerification(rec, req)

	// The mux enforces method; direct invocation returns 400 for invalid hub.mode.
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing hub.mode on direct call, got %d", rec.Code)
	}
}

// ─── Event / Step C (Bot) Tests ──────────────────────────────────────────────

// TestHandleEvent_Greeting checks that a greeting triggers the button reply.
func TestHandleEvent_Greeting(t *testing.T) {
	greetingInputs := []string{"Hello", "hello", "Hai", "HAI", "Hi", "Halo", "hey"}

	for _, greeting := range greetingInputs {
		t.Run("greeting="+greeting, func(t *testing.T) {
			sender := &mockSender{}
			handler := newTestHandler("valid-token", sender)

			req := httptest.NewRequest(http.MethodPost, "/webhook",
				strings.NewReader(webhookBody("15559876543", "Alice", greeting)))
			rec := httptest.NewRecorder()
			handler.HandleEvent(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
			// Should have sent exactly 1 button message.
			if len(sender.messages) != 1 {
				t.Fatalf("expected 1 message, got %d", len(sender.messages))
			}
			if !sender.messages[0].isButton {
				t.Error("expected a button message for greeting")
			}
			if !strings.Contains(sender.messages[0].text, "Alice") {
				t.Errorf("reply should contain customer name, got: %s", sender.messages[0].text)
			}
		})
	}
}

// TestHandleEvent_Greeting_DefaultName checks that unknown contacts get "Customer" in the reply.
func TestHandleEvent_Greeting_DefaultName(t *testing.T) {
	sender := &mockSender{}
	handler := newTestHandler("valid-token", sender)

	// WA profile name is empty → should fall back to "Customer".
	req := httptest.NewRequest(http.MethodPost, "/webhook",
		strings.NewReader(webhookBody("19991112222", "", "Hello")))
	rec := httptest.NewRecorder()
	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sender.messages))
	}
	// Name defaults to "Customer" when WA profile is empty.
	if !strings.Contains(sender.messages[0].text, "Customer") {
		t.Errorf("reply should contain 'Customer', got: %s", sender.messages[0].text)
	}
}

// TestHandleEvent_Escalation_Text checks that escalation phrases switch handler to "human".
func TestHandleEvent_Escalation_Text(t *testing.T) {
	escalationInputs := []string{"ask other question", "Ask Other Question", "talk to human"}

	for _, phrase := range escalationInputs {
		t.Run("phrase="+phrase, func(t *testing.T) {
			store := newMockStore()
			sender := &mockSender{}
			handler := newTestHandlerWithStore("valid-token", sender, store)

			req := httptest.NewRequest(http.MethodPost, "/webhook",
				strings.NewReader(webhookBody("15559876543", "Bob", phrase)))
			rec := httptest.NewRecorder()
			handler.HandleEvent(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}

			// Session must now be "human".
			session, _ := store.GetActiveSession(context.Background(), "15559876543")
			if session == nil {
				t.Fatal("expected an open session to exist")
			}
			if session.CurrentHandler != "human" {
				t.Errorf("expected handler=human, got %s", session.CurrentHandler)
			}

			// Bot sends an acknowledgement message to the customer.
			if len(sender.messages) == 0 {
				t.Error("expected an ack message to be sent to the customer")
			}
		})
	}
}

// TestHandleEvent_Escalation_Button checks that tapping the escalation button escalates.
func TestHandleEvent_Escalation_Button(t *testing.T) {
	store := newMockStore()
	sender := &mockSender{}
	handler := newTestHandlerWithStore("valid-token", sender, store)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "123",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {"display_phone_number": "15551234567", "phone_number_id": "123456"},
					"contacts": [{"profile": {"name": "Carol"}, "wa_id": "15559876543"}],
					"messages": [{
						"from": "15559876543",
						"id": "wamid.btn1",
						"timestamp": "1234567890",
						"type": "interactive",
						"interactive": {
							"type": "button_reply",
							"button_reply": {"id": "btn_ask_other", "title": "Ask other question"}
						}
					}]
				}
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	session, _ := store.GetActiveSession(context.Background(), "15559876543")
	if session == nil {
		t.Fatal("expected an open session to exist")
	}
	if session.CurrentHandler != "human" {
		t.Errorf("expected handler=human after button escalation, got %s", session.CurrentHandler)
	}
}

// TestHandleEvent_GenericBotReply checks that non-greeting, non-escalation text gets a generic reply.
func TestHandleEvent_GenericBotReply(t *testing.T) {
	sender := &mockSender{}
	handler := newTestHandler("valid-token", sender)

	req := httptest.NewRequest(http.MethodPost, "/webhook",
		strings.NewReader(webhookBody("15559876543", "Dave", "What is the price?")))
	rec := httptest.NewRecorder()
	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected 1 generic reply, got %d", len(sender.messages))
	}
	// Must be a plain text message, not a button.
	if sender.messages[0].isButton {
		t.Error("generic reply should be plain text, not a button message")
	}
}

// ─── Step D (Human Mode) Tests ────────────────────────────────────────────────

// TestHandleEvent_HumanMode checks that messages for human-handled sessions
// are NOT auto-replied to by the bot.
func TestHandleEvent_HumanMode(t *testing.T) {
	store := newMockStore()
	sender := &mockSender{}
	handler := newTestHandlerWithStore("valid-token", sender, store)

	// Pre-seed an open human-handled session.
	store.sessions["human-session"] = &db.ChatSession{
		ID:                  "human-session",
		CustomerPhoneNumber: "15559876543",
		CurrentHandler:      "human",
		SessionStatus:       "open",
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook",
		strings.NewReader(webhookBody("15559876543", "Eve", "I need more help")))
	rec := httptest.NewRecorder()
	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// In human mode the bot must NOT send any WA message.
	if len(sender.messages) != 0 {
		t.Errorf("bot should not reply in human mode, got %d messages", len(sender.messages))
	}
}

// ─── Other Event Tests ────────────────────────────────────────────────────────

func TestHandleEvent_EmptyBody(t *testing.T) {
	handler := newTestHandler("valid-token", &mockSender{})
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.HandleEvent(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleEvent_InvalidJSON(t *testing.T) {
	handler := newTestHandler("valid-token", &mockSender{})
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{bad json"))
	rec := httptest.NewRecorder()
	handler.HandleEvent(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleEvent_WrongObject(t *testing.T) {
	handler := newTestHandler("valid-token", &mockSender{})
	body := `{"object": "not_whatsapp", "entry": []}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.HandleEvent(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleEvent_StatusOnly(t *testing.T) {
	sender := &mockSender{}
	handler := newTestHandler("valid-token", sender)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "123",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {"display_phone_number": "15551234567", "phone_number_id": "123456"},
					"statuses": [{
						"id": "wamid.abc123",
						"status": "delivered",
						"timestamp": "1234567890",
						"recipient_id": "15559876543"
					}]
				}
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if len(sender.messages) != 0 {
		t.Errorf("status-only event should not trigger any message, got %d", len(sender.messages))
	}
}

func TestHandleEvent_SenderError(t *testing.T) {
	// Even when the WA API call fails, the webhook must still return 200 to Meta.
	sender := &mockSender{err: fmt.Errorf("api error")}
	handler := newTestHandler("valid-token", sender)

	req := httptest.NewRequest(http.MethodPost, "/webhook",
		strings.NewReader(webhookBody("15559876543", "Frank", "Hello")))
	rec := httptest.NewRecorder()
	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("send failure should not propagate to Meta, expected 200, got %d", rec.Code)
	}
}

// TestHandleEvent_MethodNotAllowed verifies that the mux (not HandleEvent itself)
// enforces the method restriction. When calling HandleEvent directly with a GET
// and an empty body the handler returns 400 (bad JSON). In production, the mux
// pattern "POST /webhook" rejects non-POST requests before they reach HandleEvent.
func TestHandleEvent_MethodNotAllowed(t *testing.T) {
	handler := newTestHandler("valid-token", &mockSender{})
	// Calling HandleEvent directly with an empty body → 400 Bad Request (invalid JSON).
	req := httptest.NewRequest(http.MethodGet, "/webhook", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.HandleEvent(rec, req)
	// The mux enforces method; direct invocation returns 400 for empty/invalid body.
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body on direct call, got %d", rec.Code)
	}
}

// TestHandleEvent_ContactPriority checks that the DB-stored name wins over the WA profile name.
func TestHandleEvent_ContactPriority(t *testing.T) {
	store := newMockStore()
	// Pre-seed a contact with a different name than what WA sends.
	store.contacts["15559876543"] = &db.Contact{
		PhoneNumber: "15559876543",
		Name:        "VIP Customer",
	}

	sender := &mockSender{}
	handler := newTestHandlerWithStore("valid-token", sender, store)

	req := httptest.NewRequest(http.MethodPost, "/webhook",
		strings.NewReader(webhookBody("15559876543", "WhatsApp Name", "Hello")))
	rec := httptest.NewRecorder()
	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sender.messages))
	}
	if !strings.Contains(sender.messages[0].text, "VIP Customer") {
		t.Errorf("reply should use DB name 'VIP Customer', got: %s", sender.messages[0].text)
	}
}
