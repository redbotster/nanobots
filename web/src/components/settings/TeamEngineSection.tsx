import { useCallback, useEffect, useState } from "react";
import { api } from "../../lib/api";
import type { LabEnginesResponse, TeamEngine } from "../../lib/types";
import { StatusDot } from "../StatusDot";
import { Button } from "../Button";

const ENGINE_LABEL: Record<Exclude<TeamEngine, "">, string> = {
  claude: "Claude Code",
  gemini: "Gemini CLI",
};

/** Paste an Anthropic or Gemini API key — the credential a Team engine's
 * own CLI binary speaks its vendor's API with directly, separate from any
 * bot's own connected services (see docs/team.md). Needs a restart to take
 * effect, unlike everything else on this page: the key itself is resolved
 * once at daemon startup, same as every other credential this build reads
 * at boot. */
function EngineKeyRow({
  engine,
  configured,
  onSaved,
}: {
  engine: "claude" | "gemini";
  configured: boolean;
  onSaved: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [token, setToken] = useState("");
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    if (!token.trim()) return;
    setSaving(true);
    setError(null);
    try {
      const r = await api.setLabEngineKey(engine, token.trim());
      setToken("");
      setOpen(false);
      setNotice(r.notice);
      onSaved();
    } catch (e) {
      setError(String(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="py-2.5">
      <div className="flex items-center gap-3">
        <StatusDot tone={configured ? "ok" : "muted"} />
        <div className="min-w-0 flex-1">
          <div className="text-sm text-ink">{ENGINE_LABEL[engine]}</div>
          <div className="text-[11px] text-muted">
            {engine === "claude"
              ? "An Anthropic API key (console.anthropic.com)."
              : "A Gemini API key (aistudio.google.com/apikey)."}
          </div>
        </div>
        <Button variant="ghost" onClick={() => setOpen((o) => !o)}>
          {configured ? "Replace" : "Add key"}
        </Button>
      </div>
      {open && (
        <div className="mt-3 flex items-center gap-2">
          <input
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submit()}
            placeholder="Paste key…"
            autoFocus
            className="flex-1 rounded border border-edge-strong bg-void px-2.5 py-1.5 text-xs text-ink placeholder:text-muted focus:border-tron focus:outline-none"
          />
          <Button variant="primary" onClick={submit} disabled={saving || !token.trim()}>
            {saving ? "Saving…" : "Save"}
          </Button>
        </div>
      )}
      {notice && <p className="mt-2 text-[12px] text-warn">{notice} — restart nanobotd.</p>}
      {error && <p className="mt-2 text-[12px] text-danger">{error}</p>}
    </div>
  );
}

/** Which coding-agent CLI Lab delegates to, and per-role overrides.
 *
 * Independent of 1Claw and of the "Connect a service" list above: a Team
 * engine is a real external CLI (`claude`, `gemini`) that speaks its
 * vendor's API directly, not a service a bot declares. Live — a change
 * here reaches Lab's very next delegation with no restart — except the
 * keys themselves, which need one (see EngineKeyRow).
 *
 * Built after watching a real request silently run on a Gemini free-tier
 * key with a five-to-twenty-request quota and burn through it before
 * failing, with no way to see or change which engine would answer.
 */
export function TeamEngineSection() {
  const [data, setData] = useState<LabEnginesResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [savingDefault, setSavingDefault] = useState(false);
  const [savingRole, setSavingRole] = useState<string | null>(null);

  const refresh = useCallback(() => {
    api
      .labEngines()
      .then(setData)
      .catch((e) => setError(String(e)));
  }, []);

  useEffect(refresh, [refresh]);

  if (!data) return null;

  const available: Array<"claude" | "gemini"> = (["claude", "gemini"] as const).filter((e) =>
    e === "claude" ? data.claude_configured : data.gemini_configured,
  );

  const setDefault = async (engine: TeamEngine) => {
    if (!engine) return;
    setSavingDefault(true);
    setError(null);
    try {
      await api.setDefaultLabEngine(engine);
      refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setSavingDefault(false);
    }
  };

  const setRole = async (role: string, engine: TeamEngine) => {
    setSavingRole(role);
    setError(null);
    try {
      await api.setRoleLabEngine(role, engine);
      refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setSavingRole(null);
    }
  };

  return (
    <section className="mt-4 rounded-lg border border-edge-strong bg-panel p-4">
      <h2 className="font-display text-sm font-semibold text-ink">Team engine</h2>
      <p className="mt-1 text-[13px] text-muted">
        Which coding-agent CLI Lab delegates a task to (<code>designer</code>,{" "}
        <code>backend-engineer</code>, …) — a real external tool with its own vendor API key, not a
        bot's connected service.
      </p>
      <div className="mt-4 flex flex-col divide-y divide-edge">
        <EngineKeyRow engine="claude" configured={data.claude_configured} onSaved={refresh} />
        <EngineKeyRow engine="gemini" configured={data.gemini_configured} onSaved={refresh} />
      </div>

      {available.length === 0 ? (
        <p className="mt-4 text-[12px] text-warn">
          Add a key above, then restart nanobotd, before Lab can delegate anything.
        </p>
      ) : (
        <>
          <div className="mt-4 flex items-center gap-3">
            <div className="text-sm text-ink">Default</div>
            <select
              value={data.default_engine}
              disabled={savingDefault}
              onChange={(e) => void setDefault(e.target.value as TeamEngine)}
              className="rounded border border-edge-strong bg-void px-2 py-1 text-[12px] text-ink focus:border-tron focus:outline-none disabled:opacity-60"
            >
              {available.map((e) => (
                <option key={e} value={e}>
                  {ENGINE_LABEL[e]}
                </option>
              ))}
            </select>
            <span className="text-[11px] text-muted">used by any role with no override below</span>
          </div>

          {data.roles.length === 0 ? (
            <p className="mt-3 text-[12px] text-muted">
              No role has been delegated to yet — <code>designer</code>,{" "}
              <code>backend-engineer</code>, and the rest appear here (with their own engine picker)
              the first time Lab hands them a task.
            </p>
          ) : (
            <div className="mt-3 flex flex-col divide-y divide-edge">
              {data.roles.map((r) => (
                <div key={r.role} className="flex items-center gap-3 py-2">
                  <div className="min-w-0 flex-1 text-[13px] text-ink">{r.role}</div>
                  <select
                    value={r.overridden ? r.engine : ""}
                    disabled={savingRole === r.role}
                    onChange={(e) => void setRole(r.role, e.target.value as TeamEngine)}
                    className="rounded border border-edge-strong bg-void px-2 py-1 text-[12px] text-ink focus:border-tron focus:outline-none disabled:opacity-60"
                  >
                    <option value="">
                      (default — {ENGINE_LABEL[data.default_engine || "claude"]})
                    </option>
                    {available.map((e) => (
                      <option key={e} value={e}>
                        {ENGINE_LABEL[e]}
                      </option>
                    ))}
                  </select>
                </div>
              ))}
            </div>
          )}
        </>
      )}
      {error && <p className="mt-3 text-[12px] text-danger">{error}</p>}
    </section>
  );
}
