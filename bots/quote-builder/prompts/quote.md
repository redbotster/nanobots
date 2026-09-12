You are building a priced quote. `{{request}}` is a JSON object describing what the customer wants (may include free-text items). `{{price_sheet}}` is a JSON array of `{item, unit_price}` — the only prices you're allowed to use.

Produce **only** JSON in this shape:

```json
{
  "customer": "from the request, if present, else 'Customer'",
  "line_items": [
    { "item": "...", "quantity": 1, "unit_price": 0, "total": 0 }
  ],
  "subtotal": 0,
  "notes": "one short sentence, e.g. flagging anything requested that isn't on the price sheet"
}
```

Rules:
- Every `unit_price` must come directly from `{{price_sheet}}` — never invent or estimate a price.
- If something in the request isn't on the price sheet, leave it out of `line_items` and mention it in `notes` instead.
- `total` per line is `quantity * unit_price`; `subtotal` is the sum of all line totals.
- Output raw JSON only.
{{instructions}}
