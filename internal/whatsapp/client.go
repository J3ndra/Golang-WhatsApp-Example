package whatsapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Client sends messages via the WhatsApp Cloud API (Meta Graph API).
type Client struct {
	accessToken   string
	phoneNumberID string
	baseURL       string
	apiVersion    string
	httpClient    *http.Client
}

// NewClient creates a new WhatsApp Cloud API client.
func NewClient(accessToken, phoneNumberID, baseURL, apiVersion string) *Client {
	return &Client{
		accessToken:   accessToken,
		phoneNumberID: phoneNumberID,
		baseURL:       baseURL,
		apiVersion:    apiVersion,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SendTextMessage sends a text message to a WhatsApp recipient.
func (c *Client) SendTextMessage(to, text string) error {
	url := fmt.Sprintf("%s/%s/%s/messages", c.baseURL, c.apiVersion, c.phoneNumberID)

	reqBody := SendMessageRequest{
		MessagingProduct: "whatsapp",
		To:               to,
		Type:             "text",
		Text: &TextMessage{
			Body:       text,
			PreviewURL: false,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		var successResp SendMessageResponse
		if err := json.Unmarshal(respBytes, &successResp); err != nil {
			slog.Warn("failed to parse success response", "error", err)
		}
		if len(successResp.Messages) > 0 {
			slog.Info("message sent successfully",
				"to", to,
				"message_id", successResp.Messages[0].ID,
			)
		}
		return nil
	}

	// Try to parse as APIError
	var apiErr APIError
	if err := json.Unmarshal(respBytes, &apiErr); err != nil || apiErr.Code == 0 {
		return fmt.Errorf(
			"whatsapp api returned status %d: %s",
			resp.StatusCode,
			string(respBytes),
		)
	}

	return &apiErr
}

// SendButtonMessage sends a message with quick reply buttons to a WhatsApp recipient.
func (c *Client) SendButtonMessage(to, text string, buttons []Button) error {
	url := fmt.Sprintf("%s/%s/%s/messages", c.baseURL, c.apiVersion, c.phoneNumberID)

	interactiveButtons := make([]InteractiveButton, len(buttons))
	for i, btn := range buttons {
		interactiveButtons[i] = InteractiveButton{
			Type: "reply",
			Reply: ButtonReply{
				ID:    btn.ID,
				Title: btn.Title,
			},
		}
	}

	reqBody := SendMessageRequest{
		MessagingProduct: "whatsapp",
		To:               to,
		Type:             "interactive",
		Interactive: &InteractiveMessage{
			Type: "button",
			Body: InteractiveBody{
				Text: text,
			},
			Action: InteractiveAction{
				Buttons: interactiveButtons,
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		var successResp SendMessageResponse
		if err := json.Unmarshal(respBytes, &successResp); err != nil {
			slog.Warn("failed to parse success response", "error", err)
		}
		if len(successResp.Messages) > 0 {
			slog.Info("button message sent successfully",
				"to", to,
				"message_id", successResp.Messages[0].ID,
			)
		}
		return nil
	}

	// Try to parse as APIError
	var apiErr APIError
	if err := json.Unmarshal(respBytes, &apiErr); err != nil || apiErr.Code == 0 {
		return fmt.Errorf(
			"whatsapp api returned status %d: %s",
			resp.StatusCode,
			string(respBytes),
		)
	}

	return &apiErr
}

