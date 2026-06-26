package webhook

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/endru/kiw-test/internal/whatsapp"
)

// mockSender captures sent messages for test assertions.
type mockSender struct {
	messages []sentMessage
	err      error
}

type sentMessage struct {
	to   string
	text string
}

func (m *mockSender) SendTextMessage(to, text string) error {
	m.messages = append(m.messages, sentMessage{to: to, text: text})
	return m.err
}

func (m *mockSender) SendButtonMessage(to, text string, buttons []whatsapp.Button) error {
	m.messages = append(m.messages, sentMessage{to: to, text: text})
	return m.err
}

func TestHandleVerification(t *testing.T) {
	handler := NewHandler("valid-token", &mockSender{})

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

func TestHandleVerificationMethodNotAllowed(t *testing.T) {
	handler := NewHandler("valid-token", &mockSender{})

	req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
	rec := httptest.NewRecorder()

	handler.HandleVerification(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestHandleEvent_ValidTextMessage(t *testing.T) {
	sender := &mockSender{}
	handler := NewHandler("valid-token", sender)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "123",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {
						"display_phone_number": "15551234567",
						"phone_number_id": "123456"
					},
					"contacts": [{
						"profile": {"name": "Test User"},
						"wa_id": "15559876543"
					}],
					"messages": [{
						"from": "15559876543",
						"id": "wamid.abc123",
						"timestamp": "1234567890",
						"type": "text",
						"text": {"body": "Hello from WhatsApp!"}
					}]
				}
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("expected 1 message sent, got %d", len(sender.messages))
	}

	if sender.messages[0].to != "15559876543" {
		t.Errorf("expected to %q, got %q", "15559876543", sender.messages[0].to)
	}

	expectedText := "Sorry, auto reply message currently not available since this still on development"
	if sender.messages[0].text != expectedText {
		t.Errorf("expected text %q, got %q", expectedText, sender.messages[0].text)
	}
}

func TestHandleEvent_EmptyBody(t *testing.T) {
	handler := NewHandler("valid-token", &mockSender{})

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(""))
	rec := httptest.NewRecorder()

	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandleEvent_InvalidJSON(t *testing.T) {
	handler := NewHandler("valid-token", &mockSender{})

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{bad json"))
	rec := httptest.NewRecorder()

	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandleEvent_WrongObject(t *testing.T) {
	handler := NewHandler("valid-token", &mockSender{})

	body := `{"object": "not_whatsapp", "entry": []}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandleEvent_StatusOnly(t *testing.T) {
	sender := &mockSender{}
	handler := NewHandler("valid-token", sender)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "123",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {
						"display_phone_number": "15551234567",
						"phone_number_id": "123456"
					},
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
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if len(sender.messages) != 0 {
		t.Errorf("expected 0 messages sent, got %d", len(sender.messages))
	}
}

func TestHandleEvent_SenderError(t *testing.T) {
	sender := &mockSender{err: fmt.Errorf("api error")}
	handler := NewHandler("valid-token", sender)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "123",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {
						"display_phone_number": "15551234567",
						"phone_number_id": "123456"
					},
					"messages": [{
						"from": "15559876543",
						"id": "wamid.abc123",
						"timestamp": "1234567890",
						"type": "text",
						"text": {"body": "Hello"}
					}]
				}
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.HandleEvent(rec, req)

	// Should still return 200 — send failure should not propagate to Meta.
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHandleEvent_MultipleMessages(t *testing.T) {
	sender := &mockSender{}
	handler := NewHandler("valid-token", sender)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "123",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {
						"display_phone_number": "15551234567",
						"phone_number_id": "123456"
					},
					"messages": [
						{
							"from": "15559876543",
							"id": "wamid.1",
							"timestamp": "1234567890",
							"type": "text",
							"text": {"body": "First"}
						},
						{
							"from": "15559876543",
							"id": "wamid.2",
							"timestamp": "1234567891",
							"type": "text",
							"text": {"body": "Second"}
						}
					]
				}
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if len(sender.messages) != 2 {
		t.Fatalf("expected 2 messages sent, got %d", len(sender.messages))
	}
}

func TestHandleEvent_MethodNotAllowed(t *testing.T) {
	handler := NewHandler("valid-token", &mockSender{})

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rec := httptest.NewRecorder()

	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestHandleEvent_KohEndruMessage(t *testing.T) {
	sender := &mockSender{}
	handler := NewHandler("valid-token", sender)

	body := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "123",
			"changes": [{
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {
						"display_phone_number": "15551234567",
						"phone_number_id": "123456"
					},
					"contacts": [{
						"profile": {"name": "Koh Endru"},
						"wa_id": "6282135364500"
					}],
					"messages": [{
						"from": "6282135364500",
						"id": "wamid.abc123",
						"timestamp": "1234567890",
						"type": "text",
						"text": {"body": "Help"}
					}]
				}
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("expected 1 message sent, got %d", len(sender.messages))
	}

	if sender.messages[0].to != "6282135364500" {
		t.Errorf("expected to %q, got %q", "6282135364500", sender.messages[0].to)
	}

	expectedText := "Hello Koh Endru, what can I do for you?"
	if sender.messages[0].text != expectedText {
		t.Errorf("expected text %q, got %q", expectedText, sender.messages[0].text)
	}
}
