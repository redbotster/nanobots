package step

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteOutput writes one of a bot's output ports into its run directory, in
// the shape docs/bot-contract.md describes: `<name>.json` for a value, and
// `<name>` plus `<name>.mime` for a file.
//
// Shared by cmd/nanobot-agent, which writes these from inside a container,
// and internal/runner's in-process path, which writes them from the daemon.
// One implementation on purpose: the runner reads this directory back with
// collectOutputs, so two writers that disagreed by a suffix would produce a
// bot whose outputs vanish depending on where it happened to run.
func WriteOutput(outDir string, blobs BlobStore, name string, val any) error {
	if fv, ok := val.(FileValue); ok {
		data, err := blobs.Read(fv.URI)
		if err != nil {
			return fmt.Errorf("read blob %s: %w", fv.URI, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(outDir, name+".mime"), []byte(fv.Mime), 0o644)
	}
	b, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, name+".json"), b, 0o644)
}
