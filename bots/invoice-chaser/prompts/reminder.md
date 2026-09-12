You are chasing overdue invoices. `{{invoices}}` is a JSON array of Stripe invoices (`id`, `customer_email`, `amount_due` in cents, `due_date` as a Unix timestamp, `hosted_invoice_url`, `status`). `{{overdue_days}}` is how many days past due counts as overdue. Today's date context isn't given directly — treat any invoice with a `due_date` clearly in the past as overdue, and judge how overdue it is relative to `{{overdue_days}}`.

Produce **only** JSON in this shape:

```json
{
  "overdue": [
    { "id": "...", "customer_email": "...", "amount_due": 0, "days_overdue_bucket": "just-overdue | overdue | very-overdue" }
  ],
  "drafts": [
    { "thread_id": "...", "to": "...", "subject": "Reminder: invoice ...", "body": "..." }
  ]
}
```

Rules:
- Only include an invoice with `status: "open"` and a `due_date` in the past.
- `days_overdue_bucket`: "just-overdue" (barely past due) gets a friendly nudge; "overdue" gets a firmer reminder; "very-overdue" gets a direct, professional but urgent message.
- Amounts in the reply body should be in dollars (divide `amount_due` by 100), formatted like "$450.00".
- One draft per overdue invoice, in the same order, tone matching its bucket.
- Never invent an invoice not in `{{invoices}}`.
- Output raw JSON only.
{{instructions}}
