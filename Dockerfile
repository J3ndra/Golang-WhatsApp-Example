# Stage 1: Build binary
FROM docker.io/library/golang:1.26-alpine AS builder

WORKDIR /app

# Copy dependency files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY internal/ internal/

# Build application with optimization flags (no debug info, static binary)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o server ./cmd/server/main.go

# Stage 2: Runtime image
FROM docker.io/library/alpine:3.21

# Install ca-certificates (crucial for HTTPS webhooks/external API calls)
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/server .
COPY dashboard.html .

# Expose app port
EXPOSE 8080

# Run the app
CMD ["./server"]
