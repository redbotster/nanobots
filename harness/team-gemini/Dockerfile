# team-gemini: the sandbox internal/team runs Gemini CLI inside, when a
# Team member's engine is "gemini" instead of the default "claude" — see
# docs/team.md. Same shape as harness/foundry-agent/Dockerfile (confined by
# the container boundary, not by the CLI's own permission flags), a
# separate image only because it's a different npm package with a
# different entrypoint name, not because the sandboxing story differs.
#
# Like harness/foundry-agent, this gets real outbound network access and
# its own GEMINI_API_KEY — there's no way to proxy a full, interactive,
# multi-turn coding session through nanobotd's existing bot-callback
# architecture the way a bot's own ai.generate/service.call steps are.
FROM node:22-slim
RUN npm install -g @google/gemini-cli@0.60.0
RUN useradd --uid 10001 --create-home --shell /usr/sbin/nologin team
WORKDIR /workspace
USER team
ENTRYPOINT ["gemini"]
