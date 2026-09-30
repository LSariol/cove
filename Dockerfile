# -- Build --
FROM golang:1.27.1-alpine AS builder
WORKDIR /app
COPY . .
# Set with: COVE_VERSION=$(git describe --tags --always) docker compose up -d --build
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o cove ./cmd/cove

# -- Final --
# Pinned, so a rebuild gets the same base as before. Bump it on purpose
# (e.g. to pick up security fixes): alpine:3.24 follows 3.24.x patch releases.
FROM alpine:3.24
WORKDIR /app

# Cove runs as an ordinary user, not root. The user exists only inside the
# image; the host needs no account for it, but the bind-mounted files must be
# owned by it:  chown -R 10001:10001 /srv/server/storage/cove
RUN adduser -D -H -u 10001 cove

COPY --from=builder /app/cove /cove

USER 10001:10001

EXPOSE 2100
# API server only; open the CLI with `docker exec -it <container> /cove shell`.
CMD ["/cove", "serve"]
