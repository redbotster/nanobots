# linkedin-dm-triage

Sort LinkedIn messages into leads, reply-today, later and ignore.

`llm` harness, one `ai.generate` call.

## Where the messages come from

This bot takes `messages` as data rather than fetching them, and that is a real limitation stated plainly rather than designed around.

LinkedIn has no public API that reads a personal account's inbox. The Messages API is write-only: it can send to a first-degree connection or reply into a thread, and there is no endpoint for listing conversations or reading history. Reading a member's mailbox exists only through the Compliance Events API, which is for FINRA/SEC-registered archiving vendors. Every product that advertises "read your LinkedIn inbox" does it by driving a real logged-in session, against LinkedIn's terms.

So there is no honest live path, and there is no `services:` entry here pretending otherwise. Feed it an export, a paste, or whatever your own tooling already has. If LinkedIn ever opens the endpoint, a fetcher bot snaps into `messages` and nothing here changes.

## What it does

Every message lands in exactly one bucket with a one-line `why`. The bar for **lead** is the `icp` input, not friendliness — someone pleasant who will never buy is not a lead, and a triage bot that flatters you is worse than none.

`leads` repeats just that bucket so a downstream bot can act on it without filtering. `brief` is a few lines for the morning, and `opener` is one line to start a reply, not the reply itself.
