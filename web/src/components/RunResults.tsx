import type { Run } from "../lib/types";
import { isFileOutput } from "../lib/types";
import { api } from "../lib/api";

function mimeIcon(mime: string) {
  if (mime.includes("pdf")) return "📄";
  if (mime.startsWith("image/")) return "🖼";
  if (mime.startsWith("text/")) return "📝";
  return "📦";
}

function OutputValue({ value }: { value: unknown }) {
  if (isFileOutput(value)) {
    const href = api.blobUrl(value.uri, value.mime);
    return (
      <a
        href={href}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-1.5 rounded border border-edge-strong px-2.5 py-1 text-tron hover:bg-tron/10 hover:border-tron"
      >
        <span>{mimeIcon(value.mime)}</span>
        Open file
      </a>
    );
  }
  if (typeof value === "string") {
    return <span className="text-ink">{value}</span>;
  }
  if (typeof value === "boolean") {
    return <span className={value ? "text-ok" : "text-danger"}>{String(value)}</span>;
  }
  return (
    <pre className="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded bg-void px-2 py-1.5 text-[11px] text-muted">
      {JSON.stringify(value, null, 2)}
    </pre>
  );
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
  const bots = Object.entries(run.outputs ?? {});
  if (bots.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-center text-sm text-muted">
        Nothing produced yet — check back once a bot finishes.
      </div>
    );
  }
  return (
    <div className="h-full overflow-auto px-4 py-3">
      {bots.map(([botId, outputs]) => (
        <div key={botId} className="mb-4">
          <div className="font-display text-xs font-semibold tracking-wide text-tron">
            {botId}
          </div>
          <div className="mt-1.5 flex flex-col gap-2">
            {Object.entries(outputs).map(([port, value]) => (
              <div key={port} className="flex items-start gap-3 text-[13px]">
                <span className="w-28 shrink-0 text-muted">{port}</span>
                <OutputValue value={value} />
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
