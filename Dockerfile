# -- Build --
FROM golang:1.25.1-alpine AS builder
WORKDIR /app
COPY . .
# Set with: COVE_VERSION=$(git describe --tags --always) docker compose up -d --build
ARG VERSION=dev
RUN go build -ldflags "-X main.version=${VERSION}" -o cove ./cmd/cove

# -- Final --
FROM alpine:latest
WORKDIR /app

RUN mkdir -p /app/cove

COPY --from=builder /app/cove /cove

EXPOSE 2100
CMD ["/cove"]