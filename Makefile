# Nanobots build targets.
#
# The only one most people need is `make build`, which produces a single
# binary with the WebUI inside it. The UI is a separate step because it needs
# npm, and a Go-only contributor should not be forced through it — see
# internal/webui for what a binary without one does.

.PHONY: build ui catalog dev test verify clean

## build: the single binary, UI and catalog included
build: ui catalog
	go build -o bin/nanobots ./cmd/nanobots

## ui: compile the WebUI and copy it where //go:embed can see it
ui:
	cd web && npm ci --no-audit --no-fund && npm run build
	rm -rf internal/webui/dist
	mkdir -p internal/webui/dist
	cp -R web/dist/. internal/webui/dist/
	touch internal/webui/dist/.gitkeep

## catalog: copy bots/, examples/swarms/ and roles/roles.yaml where
## //go:embed can see them, so a binary built with this in works from any
## directory, not only from inside this checkout
catalog:
	rm -rf internal/catalog/data
	mkdir -p internal/catalog/data/bots internal/catalog/data/examples/swarms internal/catalog/data/roles
	cp -R bots/. internal/catalog/data/bots/
	cp -R examples/swarms/. internal/catalog/data/examples/swarms/
	cp roles/roles.yaml internal/catalog/data/roles/roles.yaml
	touch internal/catalog/data/.gitkeep

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
	rm -rf bin internal/webui/dist web/dist internal/catalog/data
	mkdir -p internal/webui/dist && touch internal/webui/dist/.gitkeep
	mkdir -p internal/catalog/data && touch internal/catalog/data/.gitkeep
