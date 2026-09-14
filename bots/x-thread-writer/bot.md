# x-thread-writer

Turn one idea into an X thread that reads like a person wrote it.

`llm` harness, one `ai.generate` call. Drafts only. Publishing is `post-publisher`'s job, behind its own approval gate.

`post-writer` already writes a single post for X, LinkedIn and Threads. This is the other shape: a sequence, where each post has to stand alone under 280 characters and the first one has to earn the second.

Two outputs because two things want it in different shapes. `thread` is `list<string>`, ordered, ready for `x.posts.publish_thread` or a `.*` fan-out. `preview` is the same thread as one numbered block, which is what an approval gate or a Slack message should show — a JSON array of strings is unreadable in either.

`inputs.max_posts` is a ceiling, not a target. The prompt is explicit that three good posts beat seven padded ones.
