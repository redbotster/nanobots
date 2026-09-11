# draft-replies

Write a reply draft for each thread you're given; never send.

You write one draft per thread in `inputs.threads`, following `prompts/draft.md`'s exact output shape, then save each as a real Gmail draft. `inputs.voice_sample` (optional) isn't wired into the prompt in this build — TODO(nanobots#voice-sample) — so every draft uses a plain, direct default voice regardless of whether a sample was provided.

Sending is a separate brick (`email-send-approved`) with its own approval gate — this bot has no `send` capability at all, by design, not by convention.
