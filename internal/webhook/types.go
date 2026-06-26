package webhook

// WebhookPayload is the top-level structure of a WhatsApp webhook callback.
type WebhookPayload struct {
	Object string  `json:"object"` // "whatsapp_business_account"
	Entry  []Entry `json:"entry"`
}

// Entry represents a single WhatsApp Business Account entry.
type Entry struct {
	ID      string   `json:"id"`
	Changes []Change `json:"changes"`
}

// Change represents a change within a WhatsApp Business Account.
type Change struct {
	Value Value  `json:"value"`
	Field string `json:"field"` // "messages"
}

// Value contains the messaging data for a single change event.
type Value struct {
	MessagingProduct string    `json:"messaging_product"` // "whatsapp"
	Metadata         Metadata  `json:"metadata"`
	Contacts         []Contact `json:"contacts,omitempty"`
	Messages         []Message `json:"messages,omitempty"`
	Statuses         []Status  `json:"statuses,omitempty"`
}

// Metadata contains phone number information for the business account.
type Metadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}

// Contact represents a WhatsApp user who sent a message.
type Contact struct {
	Profile Profile `json:"profile"`
	WAID    string  `json:"wa_id"`
}

// Profile contains the contact's display name.
type Profile struct {
	Name string `json:"name"`
}

// Message represents an incoming WhatsApp message.
type Message struct {
	From      string       `json:"from"`    // sender's phone number
	ID        string       `json:"id"`      // WhatsApp message ID
	Timestamp string       `json:"timestamp"`
	Type      string       `json:"type"`              // "text", "image", "audio", etc.
	Text      *TextContent `json:"text,omitempty"`
}

// TextContent holds the text body of a message.
type TextContent struct {
	Body string `json:"body"`
}

// Status represents a message status update (sent, delivered, read).
type Status struct {
	ID          string `json:"id"`           // message ID
	Status      string `json:"status"`       // "sent", "delivered", "read", "failed"
	Timestamp   string `json:"timestamp"`
	RecipientID string `json:"recipient_id"`
}
