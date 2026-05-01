# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /build

# Cache deps
COPY go.mod go.sum ./
RUN go mod download

# Build binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /notification-server ./cmd/server

# Final stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /notification-server /app/notification-server

EXPOSE 8080

ENTRYPOINT ["/app/notification-server"]
