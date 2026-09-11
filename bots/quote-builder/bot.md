# quote-builder

Turn a request into a priced quote PDF from my price sheet.

`openclaw` harness: reads the price sheet, one `ai.generate` call matches the request against it and builds a priced quote (never inventing a price not in the sheet), renders it to PDF, and saves it to Drive.

`sheets` op `rows.get` (reading a whole sheet) has no live implementation yet — `internal/google/sheets.go` only implements `rows.append` (writing) so far, since every other bot needing Sheets only ever appends. Ships on `connection: demo`, same as every other bot; this is a real, disclosed gap for whenever a bot needs a live sheet *read*, not just a write.
