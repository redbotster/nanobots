# drive-save

Save a file to a folder and return the link.

`bare` harness, one step: upload `inputs.file` to `inputs.folder`, return the new file's id and link. The counterpart to `drive-watch` — together they're how a swarm moves a file bus-to-bus through Drive without every bot needing its own upload/download logic.

TODO(nanobots#file-inputs): a `file`-typed input is only exercised against demo fixtures in this build — resolving a real `nbf://` blob reference into actual bytes for a live upload isn't wired up yet, matching Gmail/Drive's broader demo-mode status (see docs/connections.md).
