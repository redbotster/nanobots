# sheet-reporter

Answer a plain-English question about a spreadsheet and produce a short report with a chart.

`openclaw` harness: reads the sheet, one `ai.generate` call answers `inputs.question` (citing which rows/values it used) and produces simple chart data, which gets rendered to a real PNG via headless Chrome (`transform.render`'s `to: png`, `internal/step.RenderHTMLToPNG` — the same subprocess pattern as PDF rendering, just `--screenshot` instead of `--print-to-pdf`).

`sheets` op `rows.get` has no live implementation yet — the same disclosed gap `quote-builder` has (`internal/google/sheets.go` only implements `rows.append` so far). Ships on `connection: demo`, same as every other bot.
