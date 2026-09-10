# bare harness: no LLM loop, no browser — just the deterministic step
# interpreter. Distroless, non-root, meant to run with --read-only (see
# internal/runner's compose generation for the runtime flags).
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/nanobot-agent ./cmd/nanobot-agent
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/nanobot-agent ./cmd/nanobot-agent

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/nanobot-agent /nanobot-agent
USER nonroot:nonroot
ENTRYPOINT ["/nanobot-agent"]
