# receipt-filer

Pull receipts and invoices out of mail and log a row per batch.

`openclaw` harness: fetches likely receipt/invoice mail, one `ai.generate` call extracts a structured receipt per message, and logs one summary row (date, count, total) to a sheet via the real `rows.append` op.

Honest scope cut from the catalog's original brick: it also calls for saving each receipt as a file to Drive and logging one row *per receipt*. Neither is built here — extracting and saving a Gmail attachment isn't implemented in `internal/google/gmail.go` (no attachment-download op yet), and this build has no swarm-engine fan-out to append N rows from one bot run (the same documented limitation `inbox-autopilot.yaml` calls out for per-item approval). The `receipts` output port still returns the full extracted list, so a swarm could snap further processing onto it later.
