import { useEffect, useRef, useState } from "react";
import { api, subscribeLabEvents } from "../lib/api";
import { Button } from "../components/Button";
import type { LogEntry } from "../lib/types";
import { timeOf } from "../lib/relativeTime";

// Shown only before the first message — once a real conversation exists,
// suggestions for starting one are noise. Each names a thing this app can
// actually do (docs/team.md, README's own `nanobots team run` example) so
// the empty state teaches by showing, not by describing.
const STARTER_PROMPTS = [
  "What can you help me with?",
  "Add a bot that watches Stripe for failed payments and posts to Slack",
  "Explain what the morning-brief swarm does",
  "Which of my bots need Docker, and why?",
];

// Who's talking, from the log entry's own "bot" field — "you" and "lab"
// are literal speakers; "team/<role>" is that role's own agent working,
// shown distinctly since those lines are what a Team member actually did,
// not something Lab said.
function speakerOf(bot: string): { label: string; align: "left" | "right" } {
  if (bot === "you") return { label: "You", align: "right" };
  if (bot === "lab") return { label: "Lab", align: "left" };
  return { label: bot, align: "left" };
}

/**
 * Lab: one chat with the orchestrator that can delegate to a Team member
 * (internal/team) or answer directly — see context/TEAM-LAB-DESIGN.md.
 *
 * There is exactly one ongoing conversation per server, the same
 * single-tenant shape as everything else in this app — opening this page
 * always shows the same session, replayed from wherever it left off.
 */
export function LabPage() {
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [message, setMessage] = useState("");
  const [sending, setSending] = useState(false);
  // Separate from `sending` on purpose. `sending` is the POST in flight —
  // done in well under a second, since the handler kicks the work off in
  // the background and returns. `busy` is "Lab hasn't actually answered
  // yet", which a real delegation can hold for 30-90 real seconds (a
  // container starting, a real model call). Found live, not in a test:
  // without this, the input re-enabled and the Send button read "Send"
  // again within about 100ms of asking a Team member to do real work,
  // with nothing on screen to say it was still happening — inviting a
  // second message to land in the middle of the first one's answer.
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    return subscribeLabEvents((entry) => {
      setEntries((prev) => [...prev, entry]);
      // Lab's actual answer is always a bare "lab" entry with no step
      // (see internal/lab.Session.appendLab) — but a delegation also logs
      // an earlier "lab" / step "delegating" line the instant it decides
      // to hand a task off, well before that task is done. Found live:
      // treating *any* "lab" entry as "done" cleared the busy indicator
      // on that interim line instead of the real answer, re-enabling the
      // input while the actual Team agent was still working.
      //
      // Derived from every entry, not just cleared by one: this endpoint
      // replays the whole history on connect (internal/api/lab.go), and
      // this ran before that replay too — `busy` started false on mount
      // and only ever flipped true from this tab's own send(), so opening
      // (or reloading) the Lab page while a real request was still running
      // in the background showed an idle input and no "thinking" line,
      // with nothing stopping a second message from firing alongside a
      // Team agent still burning the same request quota. A turn is open
      // for every entry except the final bare "lab" line, on replay same
      // as live, so this now agrees with the transcript it is looking at
      // instead of only with what this tab itself has sent.
      setBusy(!(entry.bot === "lab" && !entry.step));
    });
  }, []);

  useEffect(() => {
    endRef.current?.scrollIntoView({ block: "end" });
  }, [entries.length, busy]);

  useEffect(() => {
    if (!busy) inputRef.current?.focus();
  }, [busy]);

  const send = async (overrideText?: string) => {
    const text = (overrideText ?? message).trim();
    if (!text) return;
    setSending(true);
    setBusy(true);
    setError(null);
    setMessage("");
    try {
      await api.sendLabMessage(text);
    } catch (e) {
      setError(String(e));
      setBusy(false);
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="grid h-full grid-rows-[1fr_auto] overflow-hidden">
      <div className="overflow-auto px-4 py-4 sm:px-6">
        {entries.length === 0 && (
          <div className="flex h-full flex-col items-center justify-center gap-4 p-8 text-center text-sm text-muted">
            <p>
              Talk to Lab about what you want a Team member to do — it'll delegate to one, or just
              answer if there's nothing to delegate.
            </p>
            <div className="flex flex-wrap justify-center gap-2">
              {STARTER_PROMPTS.map((prompt) => (
                <button
                  key={prompt}
                  type="button"
                  onClick={() => void send(prompt)}
                  disabled={busy}
                  className="rounded-full border border-edge-strong px-3 py-1.5 text-[12.5px] text-ink transition-colors hover:border-tron hover:bg-tron/10 disabled:opacity-60"
                >
                  {prompt}
                </button>
              ))}
            </div>
          </div>
        )}
        <div className="flex flex-col gap-2">
          {entries.map((entry, i) => {
            const { label, align } = speakerOf(entry.bot);
            return (
              <div
                key={i}
                className={`fade-in flex flex-col ${align === "right" ? "items-end" : "items-start"}`}
              >
                <div
                  className={`max-w-[85%] rounded-lg px-3.5 py-2 text-sm sm:max-w-[70%] ${
                    align === "right"
                      ? "bg-deep text-white"
                      : entry.bot === "lab"
                        ? "border border-edge-strong bg-panel text-ink"
                        : "border border-edge bg-panel-2 text-muted"
                  }`}
                >
                  <div className="mb-0.5 flex items-baseline gap-2 text-[11px] font-display font-semibold opacity-80">
                    <span>{label}</span>
                    {entry.step && <span className="opacity-70">{entry.step}</span>}
                    <span className="opacity-60">{timeOf(entry.time)}</span>
                  </div>
                  <div className="whitespace-pre-wrap break-words">{entry.msg}</div>
                </div>
              </div>
            );
          })}
          {busy && (
            <div className="fade-in flex flex-col items-start">
              <div className="max-w-[85%] rounded-lg border border-edge bg-panel-2 px-3.5 py-2 text-sm text-muted sm:max-w-[70%]">
                <div className="mb-0.5 text-[11px] font-display font-semibold opacity-80">Lab</div>
                <div className="flex items-center gap-1.5">
                  <span className="animate-pulse">thinking</span>
                  <span className="animate-pulse">·</span>
                  <span className="animate-pulse">·</span>
                  <span className="animate-pulse">·</span>
                </div>
              </div>
            </div>
          )}
          <div ref={endRef} />
        </div>
      </div>

      <div className="border-t border-edge px-4 py-3 sm:px-6">
        {error && <p className="mb-2 text-[12px] text-danger">Couldn't send that: {error}</p>}
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void send();
          }}
        >
          <input
            ref={inputRef}
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            placeholder={
              busy
                ? "Waiting on Lab's answer…"
                : "Ask Lab to delegate something to your Team, or just say hi"
            }
            disabled={busy}
            className="min-w-0 flex-1 rounded-md border border-edge-strong bg-panel px-3 py-2 text-sm text-ink placeholder:text-muted focus:border-tron focus:outline-none disabled:opacity-60"
          />
          <Button variant="primary" type="submit" disabled={busy || !message.trim()}>
            {sending ? "Sending…" : busy ? "Working…" : "Send"}
          </Button>
        </form>
      </div>
    </div>
  );
}
