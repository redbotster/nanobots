# email-drive-file

Email a link to a Google Drive file, once a human approves sending it.

You are a `bare` harness bot: there's no model loop here, just the steps declared in `nanobot.yaml`, run in order by the step interpreter. This file documents what those steps do for anyone reading the bot, not instructions for a language model.

1. **lookup** — fetch the Drive file's name, link, and size for `inputs.file_id`.
2. **gate** — pause and ask a human: "Send '<file name>' to `<inputs.to>`?" Nothing after this runs until they approve. This step cannot be skipped or disabled by swarm-level settings — it's the one thing this bot promises never to do without a human in the loop.
3. **send** — email `inputs.to` the subject and body (intro text plus the Drive link), optionally attaching the file.
4. **stamp** — record when the send happened.

Guardrails: read-only on Drive, write-only (send) on Gmail, 60-second runtime cap, network egress limited to `googleapis.com`.

If `gdrive`/`gmail` are running in demo mode (see `nanobot.yaml`'s `connection: demo` on both services), `lookup` and `send` return realistic fixture data instead of touching a real account — the approval gate still fires so the flow is honest about what a real run would ask.
