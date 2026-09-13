import type { Run } from "../lib/types";
import { isFileOutput } from "../lib/types";
import { api } from "../lib/api";
import { CopyButton } from "./CopyButton";

function mimeIcon(mime: string) {
  if (mime.includes("pdf")) return "📄";
  if (mime.startsWith("image/")) return "🖼";
  if (mime.startsWith("text/")) return "📝";
  return "📦";
}

/** Prose a person is meant to read — a drafted post, a summary. Rendered
 * with its real line breaks instead of the "\n\n" a JSON dump shows, which
 * is what made a content swarm's whole output unreadable. */
function TextBlock({ text }: { text: string }) {
  return (
    <div className="group relative min-w-0 flex-1">
      <div className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded bg-void px-2.5 py-2 text-[12px] leading-relaxed text-ink">
        {text}
      </div>
      <div className="absolute right-1.5 top-1.5 opacity-0 transition-opacity group-hover:opacity-100">
        <CopyButton text={text} />
      </div>
    </div>
  );
}

/** True for the shape a multi-output bot actually produces — {linkedin: "…",
 * x: "…"} — as opposed to structured data that only makes sense as JSON. */
function isStringMap(value: unknown): value is Record<string, string> {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value) &&
    Object.values(value).length > 0 &&
    Object.values(value).every((v) => typeof v === "string")
  );
}

function OutputValue({ value }: { value: unknown }) {
  if (isFileOutput(value)) {
    const href = api.blobUrl(value.uri, value.mime);
    return (
      <a
        href={href}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-1.5 rounded border border-edge-strong px-2.5 py-1 text-tron hover:border-tron hover:bg-tron/10"
      >
        <span>{mimeIcon(value.mime)}</span>
        Open file
      </a>
    );
  }
  if (typeof value === "string") {
    // Short single-line values (an id, a URL) read better inline than in a box.
    if (!value.includes("\n") && value.length < 120) {
      return (
        <span className="flex min-w-0 items-center gap-2">
          <span className="break-words text-ink">{value}</span>
          <CopyButton text={value} />
        </span>
      );
    }
    return <TextBlock text={value} />;
  }
  if (typeof value === "boolean") {
    return <span className={value ? "text-ok" : "text-danger"}>{String(value)}</span>;
  }
  if (typeof value === "number") {
    return <span className="text-ink">{value}</span>;
  }
  if (isStringMap(value)) {
    return (
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        {Object.entries(value).map(([k, v]) => (
          <div key={k} className="min-w-0">
            <div className="mb-0.5 font-display text-[10px] uppercase tracking-wider text-muted">
              {k}
            </div>
            <OutputValue value={v} />
          </div>
        ))}
      </div>
    );
  }
  const json = JSON.stringify(value, null, 2);
  return (
    <div className="group relative min-w-0 flex-1">
      <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded bg-void px-2.5 py-2 text-[11px] text-muted">
        {json}
      </pre>
      <div className="absolute right-1.5 top-1.5 opacity-0 transition-opacity group-hover:opacity-100">
        <CopyButton text={json} />
      </div>
    </div>
  );
}

/** Bots in the order they actually ran, not alphabetical. The log is the
 * record of the planner's topological order, so first appearance there is
 * the execution order — no extra API call, and it matches the story the Run
 * log tab tells right next to this one. Anything with output but no log line
 * is appended rather than dropped. */
function inExecutionOrder(run: Run): [string, Record<string, unknown>][] {
  const outputs = run.outputs ?? {};
  const seen: string[] = [];
  for (const entry of run.log ?? []) {
    if (entry.bot && outputs[entry.bot] && !seen.includes(entry.bot)) {
      seen.push(entry.bot);
    }
  }
  for (const bot of Object.keys(outputs)) {
    if (!seen.includes(bot)) seen.push(bot);
  }
  return seen.map((bot) => [bot, outputs[bot]]);
}

/** What a run actually produced, per bot — the answer to "so what did I
 * get" that a log full of step names doesn't give you on its own. */
export function RunResults({ run }: { run: Run | null }) {
  if (!run) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-center text-sm text-muted">
        Results show up here once a run produces something.
      </div>
    );
  }
  const bots = inExecutionOrder(run);
  if (bots.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-center text-sm text-muted">
        Nothing produced yet — check back once a bot finishes.
      </div>
    );
  }
  return (
    <div className="h-full overflow-auto px-4 py-3">
      {bots.map(([botId, outputs], i) => (
        <div key={botId} className="mb-4">
          <div className="flex items-center gap-1.5 font-display text-xs font-semibold tracking-wide text-tron">
            <span className="text-muted">{i + 1}.</span>
            {botId}
          </div>
          <div className="mt-1.5 flex flex-col gap-2">
            {Object.entries(outputs).map(([port, value]) => (
              <div key={port} className="flex min-w-0 items-start gap-3 text-[13px]">
                <span className="w-24 shrink-0 pt-0.5 text-muted">{port}</span>
                <OutputValue value={value} />
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
