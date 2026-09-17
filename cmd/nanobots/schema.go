// `nanobots schema` — regenerate schemas/*.json from the Go types.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/redbotster/nanobots/internal/schema"
)

func runSchema(args []string) error {
	out := "schemas"
	for i := 0; i < len(args); i++ {
		if args[i] == "--out" && i+1 < len(args) {
			i++
			out = args[i]
		}
	}
	if err := writeSchema(out+"/nanobot.schema.json", schema.NanobotJSONSchema()); err != nil {
		return err
	}
	if err := writeSchema(out+"/nanoswarm.schema.json", schema.NanoswarmJSONSchema()); err != nil {
		return err
	}
	fmt.Printf("wrote %s/nanobot.schema.json and %s/nanoswarm.schema.json\n", out, out)
	return nil
}

func writeSchema(path string, doc map[string]any) error {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
