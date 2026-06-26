package whatsapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendTextMessage_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected Authorization header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected Content-Type header")
		}

		resp := SendMessageResponse{
			MessagingProduct: "whatsapp",
			Contacts:         []ContactInfo{{Input: "15559876543", WAID: "15559876543"}},
			Messages:         []MessageResult{{ID: "wamid.test123"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-token", "123456", server.URL, "v22.0")
	err := client.SendTextMessage("15559876543", "Hello!")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSendTextMessage_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(APIError{
			Code:    190,
			Message: "Invalid OAuth 2.0 Access Token",
		})
	}))
	defer server.Close()

	client := NewClient("bad-token", "123456", server.URL, "v22.0")
	err := client.SendTextMessage("15559876543", "Hello!")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 190 {
		t.Errorf("expected code 190, got %d", apiErr.Code)
	}
}

func TestSendTextMessage_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(APIError{
			Code:    4,
			Message: "Too many requests",
		})
	}))
	defer server.Close()

	client := NewClient("test-token", "123456", server.URL, "v22.0")
	err := client.SendTextMessage("15559876543", "Hello!")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != 4 {
		t.Errorf("expected code 4, got %d", apiErr.Code)
	}
}

func TestSendTextMessage_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := NewClient("test-token", "123456", server.URL, "v22.0")
	err := client.SendTextMessage("15559876543", "Hello!")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Should be a generic error, not an APIError (since the body wasn't valid APIError JSON)
	if _, ok := err.(*APIError); ok {
		t.Error("expected non-APIError for unparseable error response")
	}
}

func TestSendTextMessage_NetworkError(t *testing.T) {
	// Close the server before making the request to simulate a connection error.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.Close() // close immediately

	client := NewClient("test-token", "123456", server.URL, "v22.0")
	err := client.SendTextMessage("15559876543", "Hello!")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("token", "phone-id", "https://graph.facebook.com", "v22.0")

	if client.accessToken != "token" {
		t.Errorf("expected accessToken 'token', got %q", client.accessToken)
	}
	if client.phoneNumberID != "phone-id" {
		t.Errorf("expected phoneNumberID 'phone-id', got %q", client.phoneNumberID)
	}
	if client.baseURL != "https://graph.facebook.com" {
		t.Errorf("expected baseURL, got %q", client.baseURL)
	}
	if client.apiVersion != "v22.0" {
		t.Errorf("expected apiVersion 'v22.0', got %q", client.apiVersion)
	}
	if client.httpClient == nil {
		t.Error("expected httpClient to be set")
	}
}
