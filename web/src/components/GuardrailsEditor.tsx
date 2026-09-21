import { useState } from "react";
import { api } from "../lib/api";
import type { Guardrails } from "../lib/types";
import { Button } from "./Button";

/** Guardrails as this form edits them — every field always present, unlike
 * the wire shape where an absent field means "not declared". Converting
 * once here means the seven inputs below never have to each remember
 * their own "what if this is undefined" case. */
interface GuardrailsForm {
  pii: string;
  injectionThreshold: string;
  maxRuntimeSecs: string;
  dailyBudgetUsd: string;
  networkEgress: string;
  writesAllowed: string;
  approvalRequiredFor: string;
}

function toForm(g: Guardrails): GuardrailsForm {
  return {
    pii: g.pii || "redact",
    injectionThreshold: g.injection_threshold ? String(g.injection_threshold) : "",
    maxRuntimeSecs: g.max_runtime_secs ? String(g.max_runtime_secs) : "",
    dailyBudgetUsd: g.daily_budget_usd ? String(g.daily_budget_usd) : "",
    networkEgress: (g.network_egress ?? []).join(", "),
    writesAllowed: (g.writes_allowed ?? []).join(", "),
    approvalRequiredFor: (g.approval_required_for ?? []).join(", "),
  };
}

function splitList(s: string): string[] {
  return s
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean);
}

function toGuardrails(f: GuardrailsForm): Guardrails {
  return {
    pii: f.pii,
    injection_threshold: Number(f.injectionThreshold) || 0,
    max_runtime_secs: Number(f.maxRuntimeSecs) || 0,
    daily_budget_usd: Number(f.dailyBudgetUsd) || 0,
    network_egress: splitList(f.networkEgress),
    writes_allowed: splitList(f.writesAllowed),
    approval_required_for: splitList(f.approvalRequiredFor),
  };
}

function formsEqual(a: GuardrailsForm, b: GuardrailsForm): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

/** Edits the seven fields in a bot's own `guardrails:` block — what a bot
 * is allowed to do, everywhere it's used, not what one swarm overrides it
 * to. Same "shipped with" / "put back" shape as InstructionsEditor, kept
 * as its own component rather than folded into that one: these are
 * loosenable safety settings, not a suggestion, and every bot has them
 * (even one with no ai.generate step and so no instructions to tune at
 * all), so the two need to be offered independently.
 */
export function GuardrailsEditor({
  botId,
  current,
  shipped,
  tuned,
  startOpen = false,
  onCancel,
  onChanged,
}: {
  botId: string;
  current: Guardrails;
  /** What it shipped with — only meaningful when tuned is true. */
  shipped: Guardrails;
  tuned: boolean;
  startOpen?: boolean;
  onCancel?: () => void;
  onChanged: () => void;
}) {
  const [open, setOpen] = useState(startOpen);
  const [form, setForm] = useState(() => toForm(current));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dirty = !formsEqual(form, toForm(current));

  const set = <K extends keyof GuardrailsForm>(key: K, value: GuardrailsForm[K]) =>
    setForm((f) => ({ ...f, [key]: value }));

  const save = async (f: GuardrailsForm) => {
    setSaving(true);
    setError(null);
    try {
      await api.setBotGuardrails(botId, toGuardrails(f));
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setSaving(false);
    }
  };

  if (!open) {
    return (
      <button
        onClick={() => {
          setForm(toForm(current));
          setOpen(true);
        }}
        className="mt-3 w-full rounded border border-edge px-2.5 py-1.5 text-left text-[11px] text-muted transition-colors hover:border-tron hover:text-ink"
        title="What this bot is allowed to do, everywhere it's used"
      >
        <span className="text-muted/70">guardrails · </span>
        {current.pii ?? "redact"}
        {current.max_runtime_secs ? `, ${current.max_runtime_secs}s max` : ""}
        {current.daily_budget_usd ? `, $${current.daily_budget_usd}/day` : ""}
      </button>
    );
  }

  return (
    <div className="mt-3 rounded border border-edge-strong p-2">
      <label className="block text-[10px] uppercase tracking-wider text-muted">
        What this bot is allowed to do
      </label>

      <div className="mt-2 grid grid-cols-2 gap-2">
        <label className="text-[11px] text-muted">
          PII policy
          <select
            value={form.pii}
            onChange={(e) => set("pii", e.target.value)}
            className="mt-0.5 w-full rounded border border-edge-strong bg-void px-2 py-1 text-[12px] text-ink focus:border-tron focus:outline-none"
          >
            <option value="redact">redact</option>
            <option value="block">block</option>
            <option value="allow">allow</option>
          </select>
        </label>
        <label className="text-[11px] text-muted">
          Injection threshold
          <input
            type="number"
            min={0}
            max={1}
            step={0.05}
            value={form.injectionThreshold}
            onChange={(e) => set("injectionThreshold", e.target.value)}
            placeholder="platform default"
            className="mt-0.5 w-full rounded border border-edge-strong bg-void px-2 py-1 text-[12px] text-ink placeholder:text-muted/50 focus:border-tron focus:outline-none"
          />
        </label>
        <label className="text-[11px] text-muted">
          Max runtime (seconds)
          <input
            type="number"
            min={0}
            value={form.maxRuntimeSecs}
            onChange={(e) => set("maxRuntimeSecs", e.target.value)}
            placeholder="runner default"
            className="mt-0.5 w-full rounded border border-edge-strong bg-void px-2 py-1 text-[12px] text-ink placeholder:text-muted/50 focus:border-tron focus:outline-none"
          />
        </label>
        <label className="text-[11px] text-muted">
          Daily budget (USD)
          <input
            type="number"
            min={0}
            step={0.5}
            value={form.dailyBudgetUsd}
            onChange={(e) => set("dailyBudgetUsd", e.target.value)}
            placeholder="none"
            className="mt-0.5 w-full rounded border border-edge-strong bg-void px-2 py-1 text-[12px] text-ink placeholder:text-muted/50 focus:border-tron focus:outline-none"
          />
        </label>
      </div>

      <label className="mt-2 block text-[11px] text-muted">
        Network egress (comma-separated hosts)
        <input
          value={form.networkEgress}
          onChange={(e) => set("networkEgress", e.target.value)}
          placeholder="none — no web.fetch host is allowlisted"
          className="mt-0.5 w-full rounded border border-edge-strong bg-void px-2 py-1 text-[12px] text-ink placeholder:text-muted/50 focus:border-tron focus:outline-none"
        />
      </label>
      <label className="mt-2 block text-[11px] text-muted">
        Writes allowed (comma-separated services)
        <input
          value={form.writesAllowed}
          onChange={(e) => set("writesAllowed", e.target.value)}
          placeholder="none declared"
          className="mt-0.5 w-full rounded border border-edge-strong bg-void px-2 py-1 text-[12px] text-ink placeholder:text-muted/50 focus:border-tron focus:outline-none"
        />
      </label>
      <label className="mt-2 block text-[11px] text-muted">
        Approval required for (comma-separated step names, or *)
        <input
          value={form.approvalRequiredFor}
          onChange={(e) => set("approvalRequiredFor", e.target.value)}
          placeholder="none declared"
          className="mt-0.5 w-full rounded border border-edge-strong bg-void px-2 py-1 text-[12px] text-ink placeholder:text-muted/50 focus:border-tron focus:outline-none"
        />
      </label>

      <div className="mt-2 flex flex-wrap items-center gap-2">
        <Button variant="primary" onClick={() => void save(form)} disabled={saving || !dirty}>
          {saving ? "Saving…" : "Save"}
        </Button>
        {tuned && (
          <button
            onClick={() => {
              const back = toForm(shipped);
              setForm(back);
              void save(back);
            }}
            disabled={saving}
            className="rounded border border-edge px-2 py-0.5 text-[11px] text-muted transition-colors hover:border-warn hover:text-warn disabled:opacity-40"
          >
            Put back what it shipped with
          </button>
        )}
        {onCancel && (
          <button
            onClick={onCancel}
            className="text-[11px] text-muted hover:text-ink"
            disabled={saving}
          >
            Cancel
          </button>
        )}
        {error && <span className="w-full text-[11px] text-danger">{error}</span>}
      </div>
    </div>
  );
}
