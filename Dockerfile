# The nanobots server as one container.
#
# Used three ways: `docker compose up` locally, a 1Claw Cloud Runtime
# (`nanobots deploy 1claw`), and anywhere else that runs a container. It is
# the whole product — the binary with the WebUI inside it, the bot catalog,
# and the example swarms — because a nanobots install without its catalog is
# an empty app.
#
# Note what is *not* here: no credentials, and no way to bake any in. The
# daemon reads its 1Claw key from the environment at startup, and everything
# else it needs lives in a 1Claw vault. An image that could hold a secret
# would be an image nobody should push to a registry.

# --- the WebUI -------------------------------------------------------------
FROM node:22-slim AS ui
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- the binary ------------------------------------------------------------
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The UI has to be in place before the //go:embed in internal/webui is
# compiled, which is the whole reason the stages are ordered this way.
COPY --from=ui /src/web/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/nanobots ./cmd/nanobots

# --- what ships ------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/nanobots /usr/local/bin/nanobots
# The catalog and the examples are data the daemon reads at runtime, not
# things compiled in: a user can mount their own over these.
COPY --chown=nonroot:nonroot bots/ /app/bots/
COPY --chown=nonroot:nonroot examples/ /app/examples/
COPY --chown=nonroot:nonroot roles/ /app/roles/

USER nonroot:nonroot
EXPOSE 7474

# 0.0.0.0 rather than the loopback default, because in a container loopback
# means "this container" and nothing could ever reach it. The security
# argument for binding loopback locally (an unauthenticated callback surface
# that was never meant to leave the machine) becomes the deployer's problem
# here: put it behind 1Claw's runtime auth, or a proxy, and never on an open
# port. docs/hosting.md says so at greater length.
ENTRYPOINT ["nanobots", "up", "--addr", "0.0.0.0:7474"]
