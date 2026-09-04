# --- Stage 1: build the Go binary ---
# Matches go.mod's `go 1.25.5` directive.
FROM golang:1.25 AS build
WORKDIR /src

# Cache dependency downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY config ./config

# CGO_ENABLED=0: nothing in go.mod (alpaca-trade-api-go, mcp-go,
# shopspring/decimal, godotenv) touches cgo — PocketBase is a separate
# service reached over HTTP, not an embedded SQLite driver in this
# binary. A static binary keeps the runtime stage below simpler.
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/tradeguard ./cmd/server

# --- Stage 2: runtime ---
# This can't be a plain golang/distroless image: internal/mcpclient
# launches `alpaca-mcp-server` as a literal stdio subprocess
# (client.NewStdioMCPClient("alpaca-mcp-server", env) in
# internal/mcpclient/client.go), not over the network — same thing
# docker-compose.yml's comment on the alpaca-mcp service already notes.
# So the runtime image needs a Python environment with that package
# installed and on PATH, sitting alongside the compiled Go binary.
FROM python:3.12-slim

RUN pip install --no-cache-dir alpaca-mcp-server

COPY --from=build /out/tradeguard /usr/local/bin/tradeguard

ENTRYPOINT ["/usr/local/bin/tradeguard"]
