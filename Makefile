# Nanobots build targets.
#
# The only one most people need is `make build`, which produces a single
# binary with the WebUI inside it. The UI is a separate step because it needs
# npm, and a Go-only contributor should not be forced through it — see
# internal/webui for what a binary without one does.

.PHONY: build ui dev test verify clean

## build: the single binary, UI included
build: ui
	go build -o bin/nanobots ./cmd/nanobots

## ui: compile the WebUI and copy it where //go:embed can see it
ui:
	cd web && npm ci --no-audit --no-fund && npm run build
	rm -rf internal/webui/dist
	mkdir -p internal/webui/dist
	cp -R web/dist/. internal/webui/dist/
	touch internal/webui/dist/.gitkeep

## dev: API only, with the Vite dev server in another terminal for hot reload
dev:
	go run ./cmd/nanobots up

## verify: everything CI runs
verify:
	go build ./...
	go vet ./...
	gofmt -l . | grep -v node_modules | (! grep .)
	go test ./... -race
	cd web && npx tsc -b && npm run lint && npm run format:check && npm run test

clean:
	rm -rf bin internal/webui/dist web/dist
	mkdir -p internal/webui/dist && touch internal/webui/dist/.gitkeep
