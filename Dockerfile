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
# An empty, correctly-owned state directory to copy into the final stage.
# Distroless has no shell, so there is no RUN to mkdir one there — and
# Docker seeds a fresh named volume from whatever the image has at the
# mountpoint, ownership included. Without this the volume arrives owned by
# root, the nonroot daemon cannot create ~/.nanobots inside it, and the
# container restart-loops on "mkdir /data/.nanobots: permission denied".
RUN mkdir -p /seed/data

# --- what ships ------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/nanobots /usr/local/bin/nanobots
# The catalog and the examples are data the daemon reads at runtime, not
# things compiled in: a user can mount their own over these.
COPY --chown=nonroot:nonroot bots/ /app/bots/
COPY --chown=nonroot:nonroot examples/ /app/examples/
COPY --chown=nonroot:nonroot roles/ /app/roles/
# Run history, blobs and memory. Mount a volume here to keep them; see
# compose.yaml, which also sets HOME=/data so ~/.nanobots resolves into it.
COPY --from=build --chown=nonroot:nonroot /seed/data /data

USER nonroot:nonroot
EXPOSE 7474

# 0.0.0.0 rather than the loopback default, because in a container loopback
# means "this container" and nothing could ever reach it. The security
# argument for binding loopback locally (an unauthenticated callback surface
# that was never meant to leave the machine) becomes the deployer's problem
# here: put it behind 1Claw's runtime auth, or a proxy, and never on an open
# port. docs/hosting.md says so at greater length.
# Split so the image behaves like a CLI as well as a server. As one
# ENTRYPOINT, `docker run <image> nanobots version` appended its arguments
# to `nanobots up` — which started the daemon, bound a port and fired three
# scheduled swarms instead of printing a version. Now the default is still
# the server, and `docker run <image> version` or `... health --quiet` do
# what they say.
ENTRYPOINT ["nanobots"]
CMD ["up", "--addr", "0.0.0.0:7474"]
