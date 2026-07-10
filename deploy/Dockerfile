FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /lead-log ./cmd/lead-log

FROM alpine:3.20
RUN adduser -D appuser
USER appuser
COPY --from=builder /lead-log /lead-log
ENTRYPOINT ["/lead-log"]