# Browser Bridge

[1Claw Browser Bridge](https://docs.1claw.co/docs/agents/browser-bridge) (`@1claw/browser-bridge`) lets an agent drive a real, already-authenticated browser — or fill a credential into a page — without the secret or the session ever entering the agent's process, logs, or model provider. `internal/oneclaw/browserbridge.go` is a real, tested client for it: pairing a device, defining credential bindings, and opening a gated browser session per agent.

## What it's for here

Per `docs/connections.md`, Browser Bridge is the default `connection` strategy for any service with no viable OAuth or static-token path. It's real, working infrastructure in this build — just not exercised by any bot's `service.call` yet. Gmail/Drive don't use it because Google specifically blocks the pattern (see below); Stripe, HubSpot, X, and LinkedIn didn't end up needing it either — Stripe/HubSpot have long-lived static tokens simple enough to just paste into a vault secret, and X/LinkedIn have normal OAuth2 flows of their own (`internal/x`, `internal/linkedin`, both built on the shared `internal/oauth2pkce` core). Google Business Profile replies (`review-responder`) looked like the remaining candidate and turned out not to need it either — it's a Google OAuth scope extension now (`ScopeBusinessManage`, same pattern as Calendar), not Browser Bridge — see `docs/connections.md` for all of them. Nothing in the current catalog needs Browser Bridge as of this writing; it stays real, tested infrastructure waiting for the provider that actually does.

## The Google finding

A spike (this build's first real task, before anything else was written) tried exactly the obvious thing: pair Browser Bridge, launch an isolated Chrome profile via `startBridge` with a persistent `userDataDir`, and let the bot drive `mail.google.com` directly — no OAuth, no scope gap, just a human logging into a normal Google sign-in page once, like registering a new device.

It failed at the very first step, confirmed with screenshots: Google's sign-in page returned **"This browser or app may not be secure"** the moment it detected the CDP/automation-controlled browser — before any credential was ever involved. This is a deliberate Google anti-automation policy applied to any remote-debugging-enabled Chrome instance, not a bug in the bridge, in this code, or in the profile-persistence approach. Spoofing the automation flag to get past it would be evading Google's own bot detection, which is out of bounds regardless of the goal.

Real Gmail/Drive access is deferred to `oauth_1claw` or `oauth_native` instead (see `docs/connections.md`) — a normal OAuth consent screen doesn't touch an automated browser at all, so it doesn't hit this wall.

## Try it (for a service that isn't Google)

```
1claw browser pair "my-laptop"
1claw browser binding create <name> --url <login-url> --hosts <host>
```

Then point `internal/oneclaw.Client.PairDevice` / `CreateBrowserCredential` / `OpenBrowserSession` at the result — all three are real, tested methods (see `internal/oneclaw/browserbridge.go`), just not yet wired into any bot's `service.call` implementation in this build.
