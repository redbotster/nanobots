import { useEffect, useRef, useState } from "react";
import { api, subscribeLabEvents } from "../lib/api";
import { Button } from "../components/Button";
import type { LogEntry } from "../lib/types";

function timeOf(iso: string) {
  return new Date(iso).toLocaleTimeString([], { hour12: false });
}

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
  const [error, setError] = useState<string | null>(null);
  const endRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    return subscribeLabEvents((entry) => setEntries((prev) => [...prev, entry]));
  }, []);

  useEffect(() => {
    endRef.current?.scrollIntoView({ block: "end" });
  }, [entries.length]);

  const send = async () => {
    const text = message.trim();
    if (!text) return;
    setSending(true);
    setError(null);
    setMessage("");
    try {
      await api.sendLabMessage(text);
    } catch (e) {
      setError(String(e));
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="grid h-full grid-rows-[1fr_auto] overflow-hidden">
      <div className="overflow-auto px-4 py-4 sm:px-6">
        {entries.length === 0 && (
          <div className="flex h-full items-center justify-center p-8 text-center text-sm text-muted">
            Talk to Lab about what you want a Team member to do — it'll delegate to one, or just
            answer if there's nothing to delegate.
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
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            placeholder="Ask Lab to delegate something to your Team, or just say hi"
            disabled={sending}
            className="min-w-0 flex-1 rounded-md border border-edge-strong bg-panel px-3 py-2 text-sm text-ink placeholder:text-muted focus:border-tron focus:outline-none"
          />
          <Button variant="primary" type="submit" disabled={sending || !message.trim()}>
            {sending ? "Sending…" : "Send"}
          </Button>
        </form>
      </div>
    </div>
  );
}
