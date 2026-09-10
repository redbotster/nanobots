# openclaw harness: the step interpreter plus headless Chromium, for
# transform.render (HTML->PDF, blueprint §4 #5) and — once a real dynamic
# agent loop exists (not yet, see docs/harnesses.md) — browser-driven steps.
# Not distroless (Chromium needs a real userland), but still non-root and
# meant to run with --read-only + a tmpfs /tmp (see internal/runner).
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/nanobot-agent ./cmd/nanobot-agent
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/nanobot-agent ./cmd/nanobot-agent

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      chromium ca-certificates fonts-liberation \
    && rm -rf /var/lib/apt/lists/*
RUN useradd --uid 10001 --create-home --shell /usr/sbin/nologin nanobot
COPY --from=build /out/nanobot-agent /usr/local/bin/nanobot-agent
ENV NANOBOTS_CHROME_PATH=/usr/bin/chromium
USER nanobot
ENTRYPOINT ["/usr/local/bin/nanobot-agent"]
