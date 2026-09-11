import { useEffect, useState } from "react";
import { api } from "../lib/api";
import type { ComposeGap, SaveSwarmRequest, SwarmSummary } from "../lib/types";
import type { UIMode } from "../lib/uiMode";
import { SwarmView } from "./SwarmView";
import { BuilderPage } from "./BuilderPage";
import { FoundryJobPage } from "./FoundryJobPage";
import { Button } from "../components/Button";

type Mode =
  | { kind: "list" }
  | { kind: "view"; swarm: SwarmSummary }
  | { kind: "build"; swarm?: SwarmSummary; composedDraft?: SaveSwarmRequest }
  | { kind: "gap"; request: string; gap: ComposeGap }
  | { kind: "foundry"; jobId: string; request: string };

const EXAMPLE_PROMPT = "Help me automate a daily email recap and list it by priority";

/** The "head nanobot": describe what you want automated in plain English,
 * get a draft swarm back — already validated, never auto-saved (see
 * internal/api/compose.go). This is the primary, novice-friendly way to
 * start a swarm; the palette/canvas builder is still there for anyone who
 * wants to build or tweak by hand.
 *
 * A request the real catalog genuinely can't satisfy comes back as a gap
 * instead of a draft — onResult reports which one happened rather than
 * assuming success, so the caller can offer the foundry escalation. */
function ComposeBox({
  onResult,
}: {
  onResult: (message: string, result: { draft?: SaveSwarmRequest; gap?: ComposeGap }) => void;
}) {
  const [message, setMessage] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    if (!message.trim()) return;
    const trimmed = message.trim();
    setLoading(true);
    setError(null);
    try {
      const result = await api.compose(trimmed);
      onResult(trimmed, result);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="rounded-lg border border-edge-strong bg-panel p-5 shadow-glow-sm">
      <div className="flex items-center gap-2">
        <span className="text-lg">✨</span>
        <h2 className="font-display text-sm font-semibold text-ink">
          Describe what you want automated
        </h2>
      </div>
      <p className="mt-1 text-[13px] text-muted">
        Talk to the head nanobot — it snaps together a draft from the real catalog for you to review.
      </p>
      <div className="mt-3 flex flex-col gap-2 sm:flex-row">
        <input
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
          placeholder={EXAMPLE_PROMPT}
          disabled={loading}
          className="flex-1 rounded border border-edge-strong bg-void px-3 py-2 text-sm text-ink placeholder:text-muted focus:border-tron focus:outline-none disabled:opacity-60"
        />
        <Button variant="primary" onClick={submit} disabled={loading || !message.trim()}>
          {loading ? "Composing…" : "Automate it"}
        </Button>
      </div>
      {error && <p className="mt-2 text-[12px] text-danger">{error}</p>}
    </div>
  );
}

/** Shown when the composer declares a gap — the human opt-in gate before
 * the (slower, heavier) foundry ever runs. Never launched silently. */
function GapPanel({
  gap,
  onBuild,
  onDismiss,
}: {
  gap: ComposeGap;
  onBuild: () => void;
  onDismiss: () => void;
}) {
  return (
    <div className="rounded-lg border border-edge-strong bg-panel p-5 shadow-glow-sm">
      <h2 className="font-display text-sm font-semibold text-ink">
        The catalog can't do that yet
      </h2>
      <p className="mt-1 text-[13px] text-muted">
        Missing capability: <span className="text-ink">{gap.missing_capability}</span>
      </p>
      <p className="mt-2 text-[13px] text-muted">
        Want me to build a new bot for it? A sandboxed coding agent will
        author and self-test one — you review and approve it before it's
        ever part of the catalog.
      </p>
      <div className="mt-3 flex gap-2">
        <Button variant="primary" onClick={onBuild}>
          Build it
        </Button>
        <Button variant="ghost" onClick={onDismiss}>
          Never mind
        </Button>
      </div>
    </div>
  );
}

export function SwarmsPage({ uiMode }: { uiMode: UIMode }) {
  const [swarms, setSwarms] = useState<SwarmSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [mode, setMode] = useState<Mode>({ kind: "list" });

  const reload = () => api.listSwarms().then(setSwarms).catch((e) => setError(String(e)));
  useEffect(() => {
    reload();
  }, []);

  if (mode.kind === "view") {
    return (
      <SwarmView
        swarm={mode.swarm}
        onBack={() => setMode({ kind: "list" })}
        onEdit={() => setMode({ kind: "build", swarm: mode.swarm })}
      />
    );
  }

  if (mode.kind === "build") {
    return (
      <BuilderPage
        existing={mode.swarm}
        composedDraft={mode.composedDraft}
        onDone={(savedPath) => {
          if (!savedPath) {
            setMode({ kind: "list" });
            reload();
            return;
          }
          api.listSwarms().then((list) => {
            setSwarms(list);
            const found = list.find((s) => s.path === savedPath);
            setMode(found ? { kind: "view", swarm: found } : { kind: "list" });
          });
        }}
      />
    );
  }

  if (mode.kind === "foundry") {
    const request = mode.request;
    return (
      <FoundryJobPage
        jobId={mode.jobId}
        onDone={() => setMode({ kind: "list" })}
        onPromoted={() => {
          // The gap is filled — retry the exact same request with zero
          // further human input. It should succeed now.
          api.compose(request).then((result) => {
            setMode(result.draft ? { kind: "build", composedDraft: result.draft } : { kind: "list" });
          });
        }}
      />
    );
  }

  return (
    <div className="h-full overflow-auto p-6 sm:p-8">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="font-display text-xl font-medium text-ink">Swarms</h1>
          <p className="mt-1 text-sm text-muted">
            Saved graphs of bots snapped together — pick one to see it run, live.
          </p>
        </div>
        {uiMode === "advanced" && (
          <Button variant="ghost" onClick={() => setMode({ kind: "build" })}>
            Build manually
          </Button>
        )}
      </div>

      <div className="mt-6">
        {mode.kind === "gap" ? (
          <GapPanel
            gap={mode.gap}
            onDismiss={() => setMode({ kind: "list" })}
            onBuild={() => {
              const { request, gap } = mode;
              api
                .startFoundryJob(request, gap.missing_capability, gap.suggested_inputs, gap.suggested_outputs)
                .then((job) => setMode({ kind: "foundry", jobId: job.id, request }))
                .catch((e) => setError(String(e)));
            }}
          />
        ) : (
          <ComposeBox
            onResult={(request, result) => {
              if (result.gap) {
                setMode({ kind: "gap", request, gap: result.gap });
              } else if (result.draft) {
                setMode({ kind: "build", composedDraft: result.draft });
              }
            }}
          />
        )}
      </div>

      {error && (
        <div className="mt-6 rounded border border-danger/40 bg-danger/5 px-4 py-3 text-sm text-danger">
          Couldn't load swarms: {error}
        </div>
      )}

      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {swarms?.map((s) => (
          <button
            key={s.path}
            onClick={() => setMode({ kind: "view", swarm: s })}
            className="fade-in rounded-lg border border-edge-strong bg-panel p-4 text-left transition-shadow hover:border-tron hover:shadow-glow-sm"
          >
            <div className="flex items-start justify-between gap-2">
              <h2 className="font-display text-base font-semibold text-ink">
                {s.name}
              </h2>
              {s.services_total > 0 && (
                <span
                  className={`shrink-0 rounded-full border px-2 py-0.5 text-[10px] ${
                    s.services_live === 0
                      ? "border-edge text-muted"
                      : s.services_live === s.services_total
                        ? "border-ok/40 text-ok"
                        : "border-warn/40 text-warn"
                  }`}
                  title={`${s.services_live} of ${s.services_total} services connected to a real account`}
                >
                  {s.services_live}/{s.services_total} live
                </span>
              )}
            </div>
            <p className="mt-1.5 text-[13px] leading-snug text-muted">
              {s.description}
            </p>
          </button>
        ))}
      </div>

      {swarms?.length === 0 && (
        <p className="mt-8 text-sm text-muted">
          No swarms found in examples/swarms/.
        </p>
      )}
    </div>
  );
}
