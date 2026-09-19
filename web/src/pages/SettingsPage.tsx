import { Children, useEffect, useState } from "react";
import type React from "react";
import { api } from "../lib/api";
import { listBotsCached } from "../lib/botsCache";
import type {
  BotSummary,
  PostureResponse,
  ConnectableService,
  ConnectionStatus,
  SpendResponse,
  StatusResponse,
} from "../lib/types";
import { StatusDot } from "../components/StatusDot";
import { AppearanceSection } from "../components/settings/AppearanceSection";
import { OneClawKeySetup } from "../components/settings/OneClawKeySetup";
import { OAuthConnectRow, TokenConnectRow } from "../components/settings/ConnectRows";

const CONNECTION_LABEL: Record<string, string> = {
  demo: "Demo data",
  oauth_1claw: "1Claw sign-in",
  oauth_native: "Sign-in",
  browser: "Browser Bridge",
  api_key_vault: "API key",
};

export function SettingsPage({ status }: { status: StatusResponse | null }) {
  const [bots, setBots] = useState<BotSummary[]>([]);
  const [connections, setConnections] = useState<ConnectionStatus[] | null>(null);
  const [connectionsError, setConnectionsError] = useState<string | null>(null);

  const reloadConnections = () =>
    api
      .listConnections()
      .then(setConnections)
      .catch((e) => setConnectionsError(String(e)));

  useEffect(() => {
    listBotsCached()
      .then(setBots)
      .catch(() => {});
    reloadConnections();
  }, []);

  const isConnected = (service: ConnectableService) =>
    connections?.find((c) => c.service === service)?.connected ?? false;

  const services = new Map<string, { provider: string; connection: string; usedBy: string[] }>();
  for (const bot of bots) {
    for (const svc of bot.services) {
      const entry = services.get(svc.id) ?? {
        provider: svc.provider,
        connection: svc.connection ?? "oauth_1claw",
        usedBy: [],
      };
      entry.usedBy.push(bot.name);
      services.set(svc.id, entry);
    }
  }

  return (
    <div className="h-full overflow-auto p-5 sm:p-6">
      <h1 className="font-display text-xl font-medium text-ink">Settings</h1>

      <AppearanceSection />

      {/* One block, four rows. These were four separate cards with four
          headings, which spent about 360px of a 720px screen saying
          "everything is fine" six times. A status list should be quiet when
          nothing is wrong and loud when something is — so the explanations
          and controls below only render when they apply. */}
      <section className="mt-4 rounded-lg border border-edge-strong bg-panel p-4">
        <h2 className="font-display text-sm font-semibold text-ink">System</h2>
        <div className="mt-2 flex flex-col divide-y divide-edge">
          <StatusRow
            tone={status?.oneclaw_configured ? "ok" : "warn"}
            label="1Claw"
            detail={
              status?.oneclaw_configured
                ? "Connected — bots run live wherever their services allow it"
                : "No key configured — everything runs in demo mode"
            }
          >
            {!status?.oneclaw_configured && <OneClawKeySetup />}
          </StatusRow>

          {status?.oneclaw_configured && (
            <StatusRow
              tone={status.vault_locked ? "warn" : "ok"}
              label="Vault"
              detail={
                status.vault_locked
                  ? `Locked — ${status.vault_reason || "unlock it with your passkey"}`
                  : "Unlocked — connected credentials are readable"
              }
            />
          )}

          {status?.oneclaw_configured && <PostureRow />}

          <StatusRow
            tone={status && status.secrets_backend !== "not configured" ? "ok" : "muted"}
            label="Secrets"
            detail={status ? status.secrets_backend : "Checking…"}
          />

          <StatusRow
            tone={
              !status || status.llm_backend === "none"
                ? "warn"
                : status.llm_guardrails
                  ? "ok"
                  : "muted"
            }
            label="Model"
            detail={
              status
                ? status.llm_backend === "none"
                  ? "No model configured — bots produce their demo output"
                  : status.llm_backend
                : "Checking…"
            }
          >
            {status && status.llm_backend === "none" && (
              <p className="text-[12px] leading-snug text-muted">
                Every ai.generate step returns canned fixture text until there's a model behind it.
                Set <code className="text-ink">ONECLAW_API_KEY</code> for 1Claw token billing, or
                any one of <code className="text-ink">ANTHROPIC_API_KEY</code>,{" "}
                <code className="text-ink">OPENAI_API_KEY</code> or{" "}
                <code className="text-ink">GEMINI_API_KEY</code> — see docs/llm.md.
              </p>
            )}
            {status && status.llm_backend !== "none" && !status.llm_guardrails && (
              <p className="text-[12px] leading-snug text-muted">
                Prompts go straight to the provider. No spend ceiling, no PII redaction and no
                injection screening — 1Claw adds those, and{" "}
                <code className="text-ink">ONECLAW_API_KEY</code> switches to it.
              </p>
            )}
            <SpendLine />
          </StatusRow>

          <StatusRow
            tone={status?.memory_recall ? "ok" : "muted"}
            label="Memory"
            detail={
              status
                ? status.memory_recall
                  ? `${status.memory_backend} — bots can remember and be asked about it`
                  : `${status.memory_backend} — stores values by key`
                : "Checking…"
            }
          >
            {status && !status.memory_recall && (
              <p className="text-[12px] leading-snug text-muted">
                {status.memory_recall_bots.length > 0 ? (
                  <>
                    <span className="text-ink">{status.memory_recall_bots.join(", ")}</span>{" "}
                    {status.memory_recall_bots.length === 1 ? "asks" : "ask"} memory what happened
                    before, and {status.memory_recall_bots.length === 1 ? "is" : "are"} running
                    without an answer — falling back to static rules instead of what you've actually
                    done. Runs still succeed; they're just worse.{" "}
                  </>
                ) : (
                  <>No bot currently asks memory a question. </>
                )}
                Set <code className="text-ink">NANOBOTS_MEMORY=honcho</code> with a{" "}
                <code className="text-ink">HONCHO_URL</code> to change it — see docs/memory.md.
              </p>
            )}
          </StatusRow>

          <StatusRow
            tone={status?.docker_available ? "ok" : "warn"}
            label="Docker"
            // "Running — every bot gets its own container" was true until 34
            // of the 39 bots moved in-process, and then it was a claim this
            // page made that the run log contradicted every run.
            detail={
              status?.docker_available
                ? "Running — the bots that render a PDF or a chart have their container"
                : `${status?.docker_reason ?? "Checking…"} — most bots run without it; the ones that render a PDF or a chart need it`
            }
          />
        </div>
      </section>

      {/* Google/X/LinkedIn are OAuth flows and still need a 1Claw account
          to hold the resulting refresh token — see docs/secrets.md. The
          four pasted-token rows below don't: they write through whichever
          secrets backend this deployment resolved (1Claw vault, OS
          keychain, or an encrypted local file), so they render either way.
          This used to be one block gated entirely on 1Claw being
          configured, which hid the only UI for connecting GitHub or Slack
          from someone who had deliberately chosen not to use 1Claw at all —
          exactly the case internal/secrets exists for. */}
      <section className="mt-4 rounded-lg border border-edge-strong bg-panel p-4">
        <h2 className="font-display text-sm font-semibold text-ink">Connect a service</h2>
        <p className="mt-1 text-[13px] text-muted">
          A pasted token goes straight to{" "}
          {status?.secrets_backend ?? "your configured secrets backend"} — never onto this machine's
          disk in the clear, never into a bot container.
          {!status?.oneclaw_configured &&
            " Google, X and LinkedIn are OAuth sign-ins and still need a 1Claw account; the four below don't."}
        </p>
        {connectionsError && <p className="mt-3 text-[13px] text-danger">{connectionsError}</p>}
        <div className="mt-4 flex flex-col divide-y divide-edge">
          {status?.oneclaw_configured && (
            <>
              <OAuthConnectRow
                provider="google"
                label="Google"
                hint="Gmail, Drive, Sheets, Calendar — opens your browser to sign in"
                connected={isConnected("google")}
                onConnected={reloadConnections}
                connectFn={api.connectGoogleStart}
              />
              <OAuthConnectRow
                provider="x"
                label="X"
                hint="Posting to X — opens your browser to sign in"
                connected={isConnected("x")}
                onConnected={reloadConnections}
                connectFn={api.connectXStart}
              />
              <OAuthConnectRow
                provider="linkedin"
                label="LinkedIn"
                hint="Posting to LinkedIn — opens your browser to sign in"
                connected={isConnected("linkedin")}
                onConnected={reloadConnections}
                connectFn={api.connectLinkedInStart}
              />
            </>
          )}
          <TokenConnectRow
            service="slack"
            label="Slack"
            hint="A bot token (xoxb-...) from api.slack.com/apps, scoped to chat:write."
            connected={isConnected("slack")}
            onConnected={reloadConnections}
          />
          <TokenConnectRow
            service="github"
            label="GitHub"
            hint="A personal access token, scoped to repo (or public_repo for public repos only)."
            connected={isConnected("github")}
            onConnected={reloadConnections}
          />
          <TokenConnectRow
            service="stripe"
            label="Stripe"
            hint="A secret key from dashboard.stripe.com/apikeys."
            connected={isConnected("stripe")}
            onConnected={reloadConnections}
          />
          <TokenConnectRow
            service="hubspot"
            label="HubSpot"
            hint="A private app token, scoped to crm.objects.contacts.read/.write."
            connected={isConnected("hubspot")}
            onConnected={reloadConnections}
          />
        </div>
      </section>

      <section className="mt-4 rounded-lg border border-edge-strong bg-panel p-4">
        <h2 className="font-display text-sm font-semibold text-ink">Services in use</h2>
        <p className="mt-1 text-[13px] text-muted">
          How each bot's declared services get connected — every service resolves to one of a few
          standard strategies, never a raw API key in a bot's hands.
        </p>
        <div className="mt-4 flex flex-col divide-y divide-edge">
          {[...services.entries()].map(([id, s]) => (
            <div key={id} className="flex items-center gap-3 py-2.5">
              <StatusDot tone={s.connection === "demo" ? "warn" : "ok"} />
              <div className="min-w-0 flex-1">
                <div className="text-sm text-ink">
                  {id} <span className="text-muted">· {s.provider}</span>
                </div>
                <div className="text-[11px] text-muted">used by {s.usedBy.join(", ")}</div>
              </div>
              <div className="rounded border border-edge px-2 py-1 text-[11px] text-ink">
                {CONNECTION_LABEL[s.connection] ?? s.connection}
              </div>
            </div>
          ))}
        </div>
        <p className="mt-4 text-[12px] text-muted">
          Every bot ships on demo data by default, even after you connect a service above — switch a
          specific bot's <code>connection:</code> in its <code>nanobot.yaml</code> (or the visual
          builder, soon) to use it for real. See the README for the full story on why Gmail/Drive
          specifically needed a dedicated Google sign-in rather than 1Claw's own OAuth registry.
        </p>
      </section>
    </div>
  );
}

/** What the model calls have cost so far.
 *
 * Only 1Claw can answer this. On a direct provider key the spend is between
 * you and that provider, and this says so rather than showing a confident
 * $0.00 that actually means "no idea" — which is the worse answer, because
 * it looks like information.
 *
 * Fetched here rather than folded into /api/status: status is polled on a
 * timer by every screen, and this one costs a round trip to 1Claw. */
function SpendLine() {
  const [spend, setSpend] = useState<SpendResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    api
      .spend()
      .then(setSpend)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  }, []);

  if (error) {
    return (
      <p className="mt-1.5 text-[12px] text-muted">
        Couldn't reach 1Claw for a spend figure: {error}
      </p>
    );
  }
  if (!spend) return null;
  if (!spend.metered) {
    return (
      <p className="mt-1.5 text-[12px] leading-snug text-muted">
        Model spend isn't metered here — it's between you and your provider.
      </p>
    );
  }
  return (
    <div className="mt-2 text-[12px] leading-snug text-muted">
      <span className="text-ink">
        {spend.known ? `$${spend.spent_usd.toFixed(2)}` : "Nothing metered yet"}
      </span>{" "}
      this billing period
      {spend.known && spend.period_from && <span> · since {spend.period_from}</span>}
      {!!spend.credit_usd && <span> · ${spend.credit_usd.toFixed(2)} credit left</span>}
      {spend.warning && <p className="mt-1 text-warn">{spend.warning}</p>}
    </div>
  );
}

/** One line of system status: a dot, what it is, and how it is.
 *
 * `children` is the escape hatch for the cases that need more than a line —
 * a key-entry form, a paragraph about why memory is degraded — and renders
 * nothing at all when they don't apply, which is most of the time. That is
 * the whole point: the block should be four quiet lines when everything
 * works, and grow only where something is actually wrong. */
function StatusRow({
  tone,
  label,
  detail,
  children,
}: {
  tone: "ok" | "warn" | "muted" | "danger";
  label: string;
  detail: string;
  children?: React.ReactNode;
}) {
  const extra = Children.toArray(children).filter(Boolean);
  return (
    <div className="py-2 first:pt-1 last:pb-1">
      <div className="flex items-baseline gap-2 text-sm">
        <span className="translate-y-[-1px]">
          <StatusDot tone={tone} />
        </span>
        <span className="w-16 shrink-0 text-muted">{label}</span>
        <span className="min-w-0 text-ink">{detail}</span>
      </div>
      {extra.length > 0 && <div className="ml-[5.5rem] mt-1.5 space-y-1.5">{extra}</div>}
    </div>
  );
}

/** 1Claw's own view of this account, from its OpenTelemetry surface.
 *
 * Three numbers, and only one of them is usually interesting. The posture
 * score and open threats are a health check you want to be boring. Agents
 * is the one to act on: this repo gives every distinct bot name its own
 * 1Claw agent, plans cap how many an account can hold, and running out
 * surfaces as a 403 "Agent limit reached" from EnsureAgent in the middle of
 * a run (docs/oneclaw-bridge.md). Seeing 24 of 50 beats discovering 50 of
 * 50 when a swarm stops. */
function PostureRow() {
  const [p, setP] = useState<PostureResponse | null>(null);
  useEffect(() => {
    api
      .posture()
      .then(setP)
      .catch(() => {});
  }, []);
  if (!p || !p.configured) return null;

  if (p.error) {
    return (
      <StatusRow tone="muted" label="Posture" detail="1Claw didn't answer">
        <p className="text-[12px] leading-snug text-muted">{p.error}</p>
      </StatusRow>
    );
  }

  const cap = p.agent_limit ? `${p.agents}/${p.agent_limit}` : `${p.agents}`;
  return (
    <StatusRow
      tone={p.agents_near_cap || p.critical > 0 ? "warn" : p.threats > 0 ? "muted" : "ok"}
      label="Posture"
      detail={
        `${p.score}/100 · ${cap} agents` +
        (p.threats > 0 ? ` · ${p.threats} open threat${p.threats === 1 ? "" : "s"}` : "") +
        (p.pending > 0 ? ` · ${p.pending} awaiting approval` : "")
      }
    >
      {p.nanobots_agents > 0 && (
        <p className="text-[12px] leading-snug text-muted">
          {p.nanobots_agents} of them were made by this app — one per set of guardrails, plus a few
          fixed ones. It used to be one per bot name, which is how an account reaches the cap.
          {p.agents_near_cap && (
            <>
              {" "}
              <span className="text-warn">
                That is close to the {p.tier} plan's limit. A run that needs a new bot will fail
                with "Agent limit reached" — delete one you don't need from 1Claw to free a slot
                (docs/oneclaw-bridge.md).
              </span>
            </>
          )}
        </p>
      )}
    </StatusRow>
  );
}
