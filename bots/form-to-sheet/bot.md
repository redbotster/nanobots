# form-to-sheet

Take a webhook payload and append a row.

`bare` harness, one step: append `inputs.payload` as a row to `inputs.sheet`, returning the row that was written and its row number. Whatever validation or reshaping the payload needs before it looks like a spreadsheet row is the caller's job — this bot's contract is "append what you're given," nothing more.
