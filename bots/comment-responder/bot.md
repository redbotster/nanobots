# comment-responder

Draft replies to comments on your posts, and say which ones deserve no reply.

`llm` harness, one `ai.generate` call. Drafts only, and never sends.

Provider-agnostic by design. It takes `comments` as `list<json>` rather than fetching them, so it works with whatever got them: `x-mentions`, `linkedin-comments`, or a paste. That also means it cannot be blocked by an API it does not have.

The important output is the empty one. Every comment comes back in `replies`, in order, and a comment that should be left alone gets `"reply": ""` with a `why` explaining it — praise with no question, spam, bad faith, or a question needing a fact the bot was not given. A responder that replies to everything is a bot that makes you look like a bot.

`why` is one line per comment, and it is what a human actually reads at the approval gate. `summary` is the same thing in one sentence, for a Slack message.
