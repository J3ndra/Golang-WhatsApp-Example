# KIW-TEST (WhatsApp Business Cloud API Server)

A Go-based implementation of a WhatsApp Business Platform (Cloud API) webhook server. It handles real-time incoming messages, extracts sender metadata (names and phone numbers), and automatically replies using simple text or interactive quick-reply buttons based on custom routing rules.

## Quick Start

1. **Clone the repository** to your local machine.
2. **Install Go dependencies**:
   ```bash
   go mod tidy
   ```
3. **Configure your environment**:
   ```bash
   cp .env.example .env
   ```
   Open `.env` and fill in your Meta Developer App configuration (`WHATSAPP_ACCESS_TOKEN`, `WHATSAPP_PHONE_NUMBER_ID`, and `WHATSAPP_VERIFY_TOKEN`).
4. **Expose your local port** to the internet (Meta requires an HTTPS webhook endpoint):
   ```bash
   ngrok http 8080
   ```
5. **Run the server**:
   ```bash
   go run cmd/server/main.go
   ```

---

## Commands

| Command | Description |
|---------|-------------|
| `go run cmd/server/main.go` | Start the local server on `PORT` (defaults to 8080). |
| `go test ./...` | Run the test suite (webhook parser, mocks, client). |
| `go build -o bin/server cmd/server/main.go` | Compile the application into a binary. |

---

## Architecture

This project is structured around clean design and dependency inversion principles to decouple the webhook parser/router from the specific HTTP client implementing the Meta Graph API.

```
├── cmd/
│   └── server/
│       └── main.go           # Application entry point, router setup, and graceful shutdown
├── internal/
│   ├── config/
│   │   └── config.go         # Environment configuration parser and validation
│   ├── webhook/
│   │   ├── handler.go        # HTTP endpoints for Meta GET verification and POST event webhook
│   │   ├── handler_test.go   # Webhook handler mock tests
│   │   └── types.go          # Mappings of the WhatsApp Webhook JSON payload
│   └── whatsapp/
│       ├── client.go         # HTTP Client for Meta Graph API (Messages & Buttons)
│       ├── client_test.go    # Client tests with mock servers
│       └── types.go          # Outbound request and API response structures
```

### Key Components

*   **Webhook Handler (`internal/webhook`):** Handles verification challenges (`GET /webhook`) and incoming message event loops (`POST /webhook`). Maps the incoming `messages` array against the `contacts` array to resolve the sender's display profile name and phone number.
*   **WhatsApp API Client (`internal/whatsapp`):** Handles outbound REST calls to Meta's Graph API. Supports standard text messages (`SendTextMessage`) and quick reply reply buttons (`SendButtonMessage`).
*   **Routing Logic:** Checks `msg.From` on incoming text messages:
    *   If from `6282135364500` (Koh Endru), it replies using interactive quick-reply buttons ("Help" and "Check Status").
    *   Otherwise, it responds with a standard development placeholder message.

---

## Contributing

1. Work in short-lived feature branches branched from `main`.
2. Follow conventional commit messages (e.g., `feat: ...`, `fix: ...`, `docs: ...`).
3. Ensure all tests pass before making a commit:
   ```bash
   go test ./...
   ```
