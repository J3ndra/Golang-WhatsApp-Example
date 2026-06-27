package db

import (
	"context"
	"testing"
)

func TestSQLStore(t *testing.T) {
	// Create an in-memory database for testing
	store, err := NewSQLStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create SQL store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// 1. Test GetOrCreateContact
	t.Run("GetOrCreateContact", func(t *testing.T) {
		c, err := store.GetOrCreateContact(ctx, "+1234567890", "Alice")
		if err != nil {
			t.Fatalf("failed to get or create contact: %v", err)
		}
		if c.PhoneNumber != "+1234567890" || c.Name != "Alice" {
			t.Errorf("expected +1234567890/Alice, got %s/%s", c.PhoneNumber, c.Name)
		}

		// Verify upsert behavior (update name)
		c2, err := store.GetOrCreateContact(ctx, "+1234567890", "Alice Smith")
		if err != nil {
			t.Fatalf("failed to upsert contact: %v", err)
		}
		if c2.PhoneNumber != "+1234567890" || c2.Name != "Alice Smith" {
			t.Errorf("expected updated name Alice Smith, got %s", c2.Name)
		}
	})

	// 2. Test GetActiveSession and CreateSession
	t.Run("Sessions", func(t *testing.T) {
		// Active session should be nil initially
		session, err := store.GetActiveSession(ctx, "+1234567890")
		if err != nil {
			t.Fatalf("failed to query active session: %v", err)
		}
		if session != nil {
			t.Fatalf("expected nil active session, got %v", session)
		}

		// Create a session
		session, err = store.CreateSession(ctx, "+1234567890", "bot", "open")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}
		if session.CustomerPhoneNumber != "+1234567890" || session.CurrentHandler != "bot" || session.SessionStatus != "open" {
			t.Errorf("invalid session fields: %+v", session)
		}

		// Get active session should return it
		active, err := store.GetActiveSession(ctx, "+1234567890")
		if err != nil {
			t.Fatalf("failed to query active session: %v", err)
		}
		if active == nil || active.ID != session.ID {
			t.Fatalf("expected active session ID %s, got %v", session.ID, active)
		}

		// Update session handler to human
		err = store.UpdateSessionHandler(ctx, session.ID, "human")
		if err != nil {
			t.Fatalf("failed to update session handler: %v", err)
		}

		active, err = store.GetActiveSession(ctx, "+1234567890")
		if err != nil {
			t.Fatalf("failed to query active session: %v", err)
		}
		if active.CurrentHandler != "human" {
			t.Errorf("expected handler human, got %s", active.CurrentHandler)
		}

		// Close session
		err = store.UpdateSessionStatus(ctx, session.ID, "closed")
		if err != nil {
			t.Fatalf("failed to update session status: %v", err)
		}

		// Active session should be nil again
		active, err = store.GetActiveSession(ctx, "+1234567890")
		if err != nil {
			t.Fatalf("failed to query active session: %v", err)
		}
		if active != nil {
			t.Fatalf("expected nil active session after close, got %v", active)
		}
	})

	// 3. Test LogMessage
	t.Run("Messages", func(t *testing.T) {
		session, err := store.CreateSession(ctx, "+1234567890", "bot", "open")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		err = store.LogMessage(ctx, session.ID, "+1234567890", "system", "Hello Bot")
		if err != nil {
			t.Fatalf("failed to log incoming message: %v", err)
		}

		err = store.LogMessage(ctx, session.ID, "bot", "+1234567890", "Hello Human")
		if err != nil {
			t.Fatalf("failed to log outgoing message: %v", err)
		}
	})

	// 4. Test Ticket Form fields and state helpers
	t.Run("TicketFields", func(t *testing.T) {
		_, err := store.GetOrCreateContact(ctx, "+9999999999", "Ticket User")
		if err != nil {
			t.Fatalf("failed to create contact: %v", err)
		}

		session, err := store.CreateSession(ctx, "+9999999999", "bot", "open")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		// Initial states
		if session.BotFlowState != "idle" || session.TicketPtName != "" || session.TicketCategory != "" || session.TicketMessage != "" {
			t.Errorf("expected clean initial fields, got: %+v", session)
		}

		// Update fields
		if err := store.UpdateBotFlowState(ctx, session.ID, "awaiting_pt_name"); err != nil {
			t.Fatalf("failed to update bot flow state: %v", err)
		}
		if err := store.UpdateTicketPtName(ctx, session.ID, "PT Test"); err != nil {
			t.Fatalf("failed to update ticket pt name: %v", err)
		}
		if err := store.UpdateTicketCategory(ctx, session.ID, "Billing"); err != nil {
			t.Fatalf("failed to update ticket category: %v", err)
		}
		if err := store.UpdateTicketMessage(ctx, session.ID, "Need help with billing"); err != nil {
			t.Fatalf("failed to update ticket message: %v", err)
		}

		// Query and verify
		active, err := store.GetActiveSession(ctx, "+9999999999")
		if err != nil {
			t.Fatalf("failed to query active session: %v", err)
		}
		if active.BotFlowState != "awaiting_pt_name" {
			t.Errorf("expected state awaiting_pt_name, got %s", active.BotFlowState)
		}
		if active.TicketPtName != "PT Test" {
			t.Errorf("expected TicketPtName PT Test, got %s", active.TicketPtName)
		}
		if active.TicketCategory != "Billing" {
			t.Errorf("expected TicketCategory Billing, got %s", active.TicketCategory)
		}
		if active.TicketMessage != "Need help with billing" {
			t.Errorf("expected TicketMessage Need help with billing, got %s", active.TicketMessage)
		}
	})
}
