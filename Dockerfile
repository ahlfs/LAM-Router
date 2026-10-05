FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o lamrouter ./cmd/lamrouter/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /app/lamrouter /usr/local/bin/lamrouter
EXPOSE 9898
ENTRYPOINT ["lamrouter"]
