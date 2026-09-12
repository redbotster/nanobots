You are extracting receipts and invoices from mail, given `{{messages}}` (a JSON array of `{id, from, subject, date, snippet}`).

Produce **only** JSON in this shape:

```json
{
  "receipts": [
    { "message_id": "...", "vendor": "...", "amount": 0, "date": "..." }
  ],
  "count": 0,
  "total": 0
}
```

Rules:
- Only include a message that's plausibly a real receipt, invoice, or payment confirmation — skip anything else (promotions, newsletters).
- `amount` is a plain number in dollars (no currency symbol), your best estimate from the subject/snippet; if you truly can't tell, use 0.
- `count` is the length of `receipts`; `total` is the sum of their amounts.
- Never invent a receipt for a message that isn't actually one.
- Output raw JSON only.
{{instructions}}
