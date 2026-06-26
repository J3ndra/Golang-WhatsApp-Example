package whatsapp

import (
	"encoding/json"
	"fmt"
)

// SendMessageRequest is the request body for sending a WhatsApp message.
type SendMessageRequest struct {
	MessagingProduct string              `json:"messaging_product"`
	To               string              `json:"to"`
	Type             string              `json:"type"`
	Text             *TextMessage        `json:"text,omitempty"`
	Interactive      *InteractiveMessage `json:"interactive,omitempty"`
}

// TextMessage holds the text content and preview settings.
type TextMessage struct {
	Body       string `json:"body"`
	PreviewURL bool   `json:"preview_url"`
}

// Button represents a quick reply button defined for API consumption.
type Button struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// InteractiveMessage holds the interactive payload.
type InteractiveMessage struct {
	Type   string            `json:"type"` // "button"
	Body   InteractiveBody   `json:"body"`
	Action InteractiveAction `json:"action"`
}

// InteractiveBody contains the text of the body.
type InteractiveBody struct {
	Text string `json:"text"`
}

// InteractiveAction holds the action elements like buttons.
type InteractiveAction struct {
	Buttons []InteractiveButton `json:"buttons"`
}

// InteractiveButton represents a single quick reply button.
type InteractiveButton struct {
	Type  string      `json:"type"` // "reply"
	Reply ButtonReply `json:"reply"`
}

// ButtonReply contains the ID and Title of a button.
type ButtonReply struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// SendMessageResponse is the success response from the WhatsApp Cloud API.
type SendMessageResponse struct {
	MessagingProduct string          `json:"messaging_product"`
	Contacts         []ContactInfo   `json:"contacts"`
	Messages         []MessageResult `json:"messages"`
}

// ContactInfo contains the recipient's WhatsApp ID.
type ContactInfo struct {
	Input string `json:"input"`
	WAID  string `json:"wa_id"`
}

// MessageResult contains the server-assigned message ID.
type MessageResult struct {
	ID string `json:"id"`
}

// APIError represents an error response from the WhatsApp Cloud API.
type APIError struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Details json.RawMessage  `json:"error_data,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("whatsapp api error (code=%d): %s", e.Code, e.Message)
}
