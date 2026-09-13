import { useState } from "react";

/** Copying is the actual next action for most of what this app puts on
 * screen — a drafted post, a summary, a webhook URL you are about to paste
 * into a form service. Without this you're selecting text out of a scroll
 * box.
 *
 * Lives here rather than inside RunResults because the webhook panel needs
 * exactly the same thing, and a second copy of it would drift. */
export function CopyButton({ text, label }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      onClick={() => {
        navigator.clipboard?.writeText(text).then(
          () => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
          },
          () => {},
        );
      }}
      className="shrink-0 rounded border border-edge px-1.5 py-0.5 text-[10px] text-muted transition-colors hover:border-tron hover:text-ink"
      title="Copy to clipboard"
    >
      {copied ? "copied" : (label ?? "copy")}
    </button>
  );
}
