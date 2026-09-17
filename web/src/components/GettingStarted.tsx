import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { useRuns } from "../lib/runsFeed";
import type { StatusResponse } from "../lib/types";

/**
 * What to do next, when nothing has happened yet.
 *
 * The banners tell you what's *broken* — no Docker, no model. Nothing told
 * you what's *next* when nothing is broken, and a new install has nothing
 * broken: it lands on fifteen swarm cards and a compose box with no sense
 * of where to start.
 *
 * Three deliberate choices about what this is not:
 *
 * It is not a wizard. Every step is optional and the app works without any
 * of them — running on example data is a legitimate way to use this, not a
 * degraded one, and the copy says so rather than treating demo mode as a
 * failure to configure.
 *
 * It is not a checklist of everything possible. Three steps, each the thing
 * that most changes what you get: a model turns generated text real, a run
 * shows you what a swarm actually does, an account points it at your own
 * data.
 *
 * And it is not sticky. Each step ticks itself off from real state — not
 * from having clicked something — and the whole card disappears once all
 * three are done, permanently, without anyone having to dismiss it. It can
 * also be dismissed early, because someone who knows what they're doing
 * should not have to complete a tutorial to make it go away.
 */
const DISMISSED_KEY = "nanobots.gettingStarted.dismissed";

interface Step {
  done: boolean;
  title: string;
  detail: string;
  action?: { label: string; onClick: () => void };
}

export function GettingStarted({
  status,
  onOpenSettings,
}: {
  status: StatusResponse | null;
  onOpenSettings: () => void;
}) {
  const [dismissed, setDismissed] = useState(() => {
    try {
      return localStorage.getItem(DISMISSED_KEY) === "1";
    } catch {
      return false;
    }
  });
  // Expanded is only ever set by the user clicking "Show". The default is
  // derived from progress below, so someone who has done nothing still gets
  // the full card without having to ask for it.
  const [expanded, setExpanded] = useState(false);
  const [connected, setConnected] = useState<boolean | null>(null);

  // Through the shared feed, not a fetch of its own. This asked
  // `api.listRuns()` on every mount and used the answer for one boolean —
  // has anything ever run — which cost the whole 81KB run list each time
  // this card appeared, on a page it appears on every visit to Swarms.
  //
  // runsFeed exists for exactly this: one poller, ETag-aware, shared by
  // everything that wants the list. The Runs page and the approval
  // notifier were merged into it for the same reason and this was the one
  // caller left outside.
  //
  // null until the first fetch lands, which `ran` below already treats as
  // "assume it has" — a card that appears because a request has not
  // answered yet is worse than one that appears a second late.
  const runs = useRuns();
  const hasRun = runs === null ? null : runs.length > 0;

  useEffect(() => {
    api
      .listConnections()
      .then((c) => setConnected(c.some((x) => x.connected)))
      .catch(() => setConnected(null));
  }, []);

  // Anything unknown counts as done: a card that appears because a request
  // failed would be worse than one that never appears at all.
  const hasModel = !status || status.llm_backend !== "none";
  const ran = hasRun !== false;
  const linked = connected !== false;

  if (dismissed || (hasModel && ran && linked)) return null;

  const steps: Step[] = [
    {
      done: hasModel,
      title: "Point it at a model",
      detail:
        "Without one, every bot returns its example output instead of thinking. 1Claw adds a spend cap and redaction; any provider key works on its own.",
      action: hasModel ? undefined : { label: "Settings", onClick: onOpenSettings },
    },
    {
      done: ran,
      title: "Run one on example data",
      detail:
        "Every swarm below works right now against this repo's own example inbox, invoices and files — nothing of yours is read or written. Pick one and press Run to see exactly what it does.",
    },
    {
      done: linked,
      title: "Connect an account, when you want it to be real",
      detail:
        "Then the same swarm reads your actual inbox instead. Settings switches every bot using that provider over in one click, and back again.",
      action: linked ? undefined : { label: "Settings", onClick: onOpenSettings },
    },
  ];

  const dismiss = () => {
    try {
      localStorage.setItem(DISMISSED_KEY, "1");
    } catch {
      // A browser that refuses storage still gets the card closed for this
      // session, which is the part the click was asking for.
    }
    setDismissed(true);
  };

  const remaining = steps.filter((s) => !s.done);

  // Shrinks as you make progress.
  //
  // This sat at the top of the landing page at full height whatever your
  // state, so on a 720px viewport the first swarm card started at y=533 —
  // three quarters of the screen spent on chrome before the thing you came
  // for. Most of that was this card, still explaining all three steps to
  // someone who had done two of them and run 184 swarms.
  //
  // A user with nothing done still gets the full card: that is the case it
  // was written for, and the one where the detail earns its space. Once
  // anything is done it becomes one line naming what is left, which is the
  // only part still worth reading, and "Show" brings the rest back.
  if (!expanded && remaining.length < steps.length) {
    return (
      <div className="mb-3 flex items-center gap-2 rounded-lg border border-edge bg-panel/40 px-3 py-2 text-[12px]">
        <span className="text-muted">
          Getting started · {steps.length - remaining.length} of {steps.length} done
        </span>
        <span className="min-w-0 truncate text-ink">{remaining[0].title}</span>
        {remaining[0].action && (
          <button
            onClick={remaining[0].action.onClick}
            className="shrink-0 text-tron underline-offset-2 hover:underline"
          >
            {remaining[0].action.label}
          </button>
        )}
        <span className="ml-auto flex shrink-0 gap-2.5">
          <button onClick={() => setExpanded(true)} className="text-muted hover:text-ink">
            Show
          </button>
          <button onClick={dismiss} className="text-muted hover:text-ink">
            Hide
          </button>
        </span>
      </div>
    );
  }

  return (
    <div className="mb-4 rounded-lg border border-edge-strong bg-panel/60 p-5">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="font-display text-sm font-semibold text-ink">Getting started</h2>
          <p className="mt-0.5 text-[13px] leading-snug text-muted">
            Nothing here needs setting up to try. These three change what you get out of it.
          </p>
        </div>
        <button
          onClick={dismiss}
          className="shrink-0 text-[11px] text-muted transition-colors hover:text-ink"
        >
          Hide
        </button>
      </div>

      <ol className="mt-3 flex flex-col gap-2.5">
        {steps.map((s) => (
          <li key={s.title} className="flex gap-2.5">
            <span
              aria-hidden
              className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full border text-[9px] ${
                s.done ? "border-ok/60 bg-ok/15 text-ok" : "border-edge-strong text-muted"
              }`}
            >
              {s.done ? "✓" : ""}
            </span>
            <div className="min-w-0">
              <span
                className={`text-[13px] ${s.done ? "text-muted line-through decoration-muted/40" : "text-ink"}`}
              >
                {s.title}
              </span>
              {!s.done && (
                <p className="mt-0.5 text-[12px] leading-snug text-muted">
                  {s.detail}
                  {s.action && (
                    <>
                      {" "}
                      <button
                        onClick={s.action.onClick}
                        className="text-tron underline-offset-2 hover:underline"
                      >
                        {s.action.label}
                      </button>
                    </>
                  )}
                </p>
              )}
            </div>
          </li>
        ))}
      </ol>
    </div>
  );
}
