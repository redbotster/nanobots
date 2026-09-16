import { useState } from "react";
import type { SwarmSummary, WebhookDetails } from "../lib/types";
import { api } from "../lib/api";
import { CopyButton } from "./CopyButton";

/** Where a webhook swarm's URL lives.
 *
 * Webhook triggers fire now (docs/webhooks.md), and the only instruction
 * for finding the token was to `cat` a file under ~/.nanobots. That is a
 * fine answer for the person who wrote the daemon and no answer at all for
 * the person the feature is for — somebody with a form on a website who
 * needs one URL to paste into it.
 *
 * The token is fetched on click rather than carried in the swarm list. It
 * is a credential, /api/swarms is polled by every open tab, and a secret
 * that follows you around ends up in a screenshot of something else. This
 * is not a security boundary — see handleWebhookDetails — it is the
 * difference between a secret you asked for and one you didn't.
 */
export function WebhookPanel({ swarm }: { swarm: SwarmSummary }) {
  const [details, setDetails] = useState<WebhookDetails | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  // One reveal for the whole panel. Masking the token in its own field
  // while printing it in full in the curl line two rows below is not
  // masking, it is decoration.
  const [shown, setShown] = useState(false);

  const reveal = async () => {
    setLoading(true);
    setError(null);
    try {
      setDetails(await api.webhookDetails(swarm.name));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  };

  const mask = "•".repeat(32);

  return (
    <div className="mt-3 rounded-lg border border-edge bg-panel/40 p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-[12px] text-muted">
          ⚡ Runs when something posts to it — a form, another tool, a script.
        </p>
        {details ? (
          // Collapsible, so getting the URL doesn't cost you the swarm
          // graph and the Run button for the rest of the visit.
          <button
            onClick={() => {
              setDetails(null);
              setShown(false);
            }}
            className="shrink-0 rounded border border-edge px-2 py-0.5 text-[11px] text-muted transition-colors hover:border-tron hover:text-ink"
          >
            Hide
          </button>
        ) : (
          <button
            onClick={reveal}
            disabled={loading}
            className="shrink-0 rounded border border-edge px-2 py-0.5 text-[11px] text-muted transition-colors hover:border-tron hover:text-ink disabled:opacity-50"
          >
            {loading ? "…" : "Show the URL"}
          </button>
        )}
      </div>

      {error && <p className="mt-2 text-[11px] text-danger">{error}</p>}

      {details && (
        <div className="mt-2.5 space-y-2">
          <Field label="Post to" value={details.url} />
          <Field
            label="Bearer token"
            value={details.token}
            display={shown ? details.token : mask}
            onToggle={() => setShown((v) => !v)}
            shown={shown}
          />
          <div>
            <div className="mb-1 flex items-center justify-between gap-2">
              <span className="font-display text-[10px] uppercase tracking-wide text-muted/70">
                Try it
              </span>
              {/* Copy takes the real command even while it is masked on
                  screen — you paste it into a terminal, you don't read it
                  off the page. */}
              <CopyButton text={details.curl} label="copy command" />
            </div>
            <pre className="overflow-x-auto rounded bg-void px-2.5 py-2 text-[11px] leading-relaxed text-ink">
              {shown ? details.curl : details.curl.replace(details.token, mask)}
            </pre>
          </div>
          <p className="text-[11px] text-muted/70">
            The body arrives as <code className="text-muted">{"{{trigger.payload}}"}</code> in this
            swarm's input templates. Anything else posting here needs the same token — this endpoint
            starts runs, and runs send mail.
          </p>
        </div>
      )}
    </div>
  );
}

/** One copyable value. `display` differs from `value` for a secret: the URL
 * is what you paste into a config screen and the token is what you paste
 * into a password field, and they should not look alike on screen. Copy
 * always takes the real value. */
function Field({
  label,
  value,
  display,
  shown,
  onToggle,
}: {
  label: string;
  value: string;
  display?: string;
  shown?: boolean;
  onToggle?: () => void;
}) {
  return (
    <div>
      <div className="mb-1 flex items-center justify-between gap-2">
        <span className="font-display text-[10px] uppercase tracking-wide text-muted/70">
          {label}
        </span>
        <div className="flex items-center gap-1.5">
          {onToggle && (
            <button
              onClick={onToggle}
              className="shrink-0 rounded border border-edge px-1.5 py-0.5 text-[10px] text-muted transition-colors hover:border-tron hover:text-ink"
            >
              {shown ? "hide" : "show"}
            </button>
          )}
          <CopyButton text={value} />
        </div>
      </div>
      <div className="overflow-x-auto whitespace-nowrap rounded bg-void px-2.5 py-1.5 font-mono text-[11px] text-ink">
        {display ?? value}
      </div>
    </div>
  );
}
