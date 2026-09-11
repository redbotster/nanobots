# invoice-chaser

Find overdue invoices and draft reminders that escalate in tone by age.

`openclaw` harness: fetches open Stripe invoices, one `ai.generate` call decides which are actually overdue (past `inputs.overdue_days`) and drafts a reminder for each — polite for a few days late, firmer the longer it's been. Drafts only; sending goes through `email-send-approved`'s own unconditional approval gate.

Ships on `connection: demo` like every other bot, but `internal/stripe` and `internal/step/stripe_live.go` are real — switching this service to a live connection and connecting a Stripe secret key (WebUI Settings, or `nanobots connect stripe`) makes this genuinely live.
