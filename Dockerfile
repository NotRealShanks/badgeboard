# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install security certificates
RUN apk add --no-cache ca-certificates tzdata

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source tree and compile static binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=v0.2.0 -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo 'release') -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /app/badgeboard ./cmd/badgeboard

# Final minimal distroless container
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /tmp

# Copy timezone data, certificates, and compiled binary
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/badgeboard /badgeboard

# Non-root user 65532:65532 is built into distroless nonroot
USER nonroot:nonroot

EXPOSE 8080

ENV PORT=8080

ENTRYPOINT ["/badgeboard", "serve", "--addr", "0.0.0.0:8080", "--hosted"]

