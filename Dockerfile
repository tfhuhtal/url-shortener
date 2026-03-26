FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
COPY vendor ./vendor
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOFLAGS=-mod=vendor go build -o /bin/server ./cmd/server

FROM alpine:3.21

RUN adduser -D -u 10001 appuser
USER appuser

WORKDIR /app
COPY --from=builder /bin/server /server

EXPOSE 8080

ENTRYPOINT ["/server"]
