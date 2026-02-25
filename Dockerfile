FROM golang:1.25.7-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/stratum ./cmd/server

FROM alpine:3.20
RUN addgroup -S stratum && adduser -S -G stratum stratum \
    && apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /out/stratum /app/stratum
COPY --from=builder /src/config /app/config

USER stratum
EXPOSE 8000
ENTRYPOINT ["/app/stratum"]
