# foundry-agent: the sandbox the foundry (internal/foundry) runs a coding
# agent CLI inside, so an untrusted, tool-using coding session is confined
# by the container boundary itself rather than by CLI permission flags —
# tested and confirmed those flags alone don't reliably restrict what a
# tool-using agent's shell tool can touch (see internal/foundry/agent.go's
# package doc). The container gets exactly one writable mount (a job's git
# worktree) and one read-only mount (a job-scoped `nanobots` binary, built
# on the host before the container starts — see internal/foundry/worktree.go).
#
# Unlike every other container in this build, this one gets real outbound
# network access and its own ANTHROPIC_API_KEY — there's no way to proxy a
# full, interactive, multi-turn coding session through nanobotd's existing
# bot-callback architecture the way a bot's own ai.generate/service.call
# steps are proxied. That's a deliberate, disclosed exception, not an
# oversight (see docs/connections.md's own "what's real vs. simulated"
# pattern for how this build treats every other real gap).
FROM node:22-slim
RUN npm install -g @anthropic-ai/claude-code@2.1.268
RUN useradd --uid 10001 --create-home --shell /usr/sbin/nologin foundry
WORKDIR /workspace
USER foundry
ENTRYPOINT ["claude"]
