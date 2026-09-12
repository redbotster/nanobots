# draft-replies

Write a reply draft for each thread you're given; never send.

You write one draft per thread in `inputs.threads`, following `prompts/draft.md`'s exact output shape, then save each as a real Gmail draft. `inputs.instructions` (optional) is wired into the prompt and is how you change the voice — "write shorter", "never promise a date I haven't confirmed". `inputs.voice_sample` (a file) is still accepted and still not read; use `instructions` instead.

Sending is a separate brick (`email-send-approved`) with its own approval gate — this bot has no `send` capability at all, by design, not by convention.
