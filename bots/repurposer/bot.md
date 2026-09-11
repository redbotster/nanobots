# repurposer

Turn one long piece (blog, transcript, video notes) into a thread, a LinkedIn post, and a newsletter blurb.

`openclaw` harness, one `ai.generate` call. `source` is read as its actual text content (via the interpreter's file-input-to-text resolution, `internal/step/interpret.go`'s `resolveFileInputAsText`), not just a file reference — this bot needs the real content to repurpose, unlike bots that treat a file input as optional/decorative.
