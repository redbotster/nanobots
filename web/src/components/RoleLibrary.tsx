import { useCallback, useEffect, useState } from "react";
import { api } from "../lib/api";
import type { Role, RoleLibrary as Library } from "../lib/types";
import { Button } from "./Button";

/**
 * The role library: the perspectives a review team gets built from.
 *
 * `review-board` used to invent its team fresh from a prompt every run.
 * Good choices, but unrepeatable, invisible before the run, and with
 * nowhere to say what *your* security reviewer actually cares about. It now
 * picks from this list, and this is where you shape it.
 *
 * A role is not a bot — one `reviewer` becomes a security engineer or a
 * designer depending on what it's handed (docs/supervisors.md) — which is
 * why this sits alongside the Fleet's tuned bots rather than inside them.
 */
function RoleRow({ role, onChanged }: { role: Role; onChanged: (lib: Library) => void }) {
  const [focus, setFocus] = useState(role.focus);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dirty = focus.trim() !== role.focus.trim();

  useEffect(() => setFocus(role.focus), [role.focus]);

  const run = async (fn: () => Promise<Library>) => {
    setBusy(true);
    setError(null);
    try {
      onChanged(await fn());
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className={`rounded-lg border p-4 ${
        role.retired ? "border-edge bg-panel/30 opacity-60" : "border-edge-strong bg-panel"
      }`}
    >
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <span className="font-display text-sm font-semibold text-ink">{role.name}</span>
        {role.custom && <span className="text-[11px] text-tron">yours</span>}
        {role.retired && <span className="text-[11px] text-warn">switched off</span>}
        {role.shipped && !role.retired && <span className="text-[11px] text-muted">edited</span>}
      </div>

      <textarea
        value={focus}
        onChange={(e) => setFocus(e.target.value)}
        rows={3}
        maxLength={500}
        aria-label={`What ${role.name} looks at`}
        className="mt-2 w-full resize-none rounded border border-edge bg-void px-2.5 py-1.5 text-[13px] leading-snug text-ink focus:border-tron focus:outline-none"
      />

      <div className="mt-1.5 flex flex-wrap items-center gap-2 text-[11px]">
        <button
          onClick={() => void run(() => api.setRole(role.id, { name: role.name, focus }))}
          disabled={busy || !dirty}
          className="rounded border border-edge-strong px-2 py-0.5 text-ink transition-colors hover:border-tron disabled:opacity-40"
        >
          {busy ? "Saving…" : "Save"}
        </button>
        <button
          onClick={() => void run(() => api.setRole(role.id, { retired: !role.retired }))}
          disabled={busy}
          className="rounded border border-edge px-2 py-0.5 text-muted transition-colors hover:border-warn hover:text-warn disabled:opacity-40"
        >
          {role.retired ? "Switch back on" : "Switch off"}
        </button>
        {(role.shipped || role.custom) && (
          <button
            onClick={() => void run(() => api.resetRole(role.id))}
            disabled={busy}
            className="rounded border border-edge px-2 py-0.5 text-muted transition-colors hover:border-warn hover:text-warn disabled:opacity-40"
          >
            {/* Two different actions behind one honest label: a shipped
                role goes back, one you wrote is removed. */}
            {role.custom ? "Remove" : "Put back what it shipped with"}
          </button>
        )}
        {error && <span className="text-danger">{error}</span>}
      </div>

      {role.shipped && (
        <p className="mt-1.5 text-[11px] leading-snug text-muted/70">
          <span className="text-muted">shipped with · </span>
          {role.shipped}
        </p>
      )}
    </div>
  );
}

function AddRole({ onChanged }: { onChanged: (lib: Library) => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [focus, setFocus] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // The id is derived rather than asked for: it exists so an edit can find
  // the role again, and making someone invent a slug is a question with
  // only one sensible answer.
  const id = name.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      onChanged(await api.setRole(id, { name: name.trim(), focus }));
      setName("");
      setFocus("");
      setOpen(false);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        className="rounded-lg border border-dashed border-edge-strong p-4 text-left text-[13px] text-muted transition-colors hover:border-tron hover:text-ink"
      >
        + Add a perspective your team actually uses
      </button>
    );
  }

  return (
    <div className="rounded-lg border border-tron/60 bg-panel p-4">
      <input
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="Legal"
        aria-label="Role name"
        className="w-full rounded border border-edge bg-void px-2.5 py-1.5 text-sm text-ink placeholder:text-muted focus:border-tron focus:outline-none"
      />
      <textarea
        value={focus}
        onChange={(e) => setFocus(e.target.value)}
        rows={3}
        maxLength={500}
        placeholder="One line: what this role looks at that no other role would."
        aria-label="What this role looks at"
        className="mt-2 w-full resize-none rounded border border-edge bg-void px-2.5 py-1.5 text-[13px] leading-snug text-ink placeholder:text-muted focus:border-tron focus:outline-none"
      />
      <div className="mt-2 flex items-center gap-2 text-[11px]">
        <Button variant="primary" onClick={submit} disabled={busy || !id || !focus.trim()}>
          {busy ? "Adding…" : "Add role"}
        </Button>
        <button onClick={() => setOpen(false)} className="text-muted hover:text-ink">
          Cancel
        </button>
        {error && <span className="text-danger">{error}</span>}
      </div>
    </div>
  );
}

export function RoleLibrarySection() {
  const [lib, setLib] = useState<Library | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [showRoster, setShowRoster] = useState(false);

  const reload = useCallback(() => {
    api.roles().then(setLib).catch((e) => setError(String(e)));
  }, []);
  useEffect(reload, [reload]);

  const active = (lib?.roles ?? []).filter((r) => !r.retired).length;

  return (
    <section className="mt-8">
      <h2 className="font-display text-xs font-semibold uppercase tracking-wider text-muted">
        Roles a review team is built from
      </h2>
      <p className="mt-1 max-w-2xl text-[13px] leading-snug text-muted">
        A review board picks {active > 0 ? `from these ${active}` : "from these"} rather than
        inventing a team each time, so the same work gets reviewed the same way twice. Each role's
        line is what its reviewer is told to look at — keep them sharp and non-overlapping, since two
        roles that would raise the same concern is one role too many.
      </p>

      {error && (
        <div className="mt-3 rounded border border-danger/40 bg-danger/5 px-4 py-3 text-sm text-danger">
          Couldn't load the roles: {error}
        </div>
      )}
      {lib?.error && (
        <div className="mt-3 rounded border border-warn/40 bg-warn/5 px-4 py-3 text-[13px] text-warn">
          {lib.error}
        </div>
      )}

      {lib === null && !error && <p className="mt-4 text-sm text-muted/60">Loading…</p>}

      {lib && (
        <>
          <div className="mt-3 grid grid-cols-1 gap-3 lg:grid-cols-2">
            {lib.roles.map((r) => (
              <RoleRow key={r.id} role={r} onChanged={setLib} />
            ))}
            <AddRole onChanged={setLib} />
          </div>

          {/* The exact text the model sees. Someone editing a role is
              trying to answer "what will this actually look like", and a
              reconstruction of it would drift from the real thing. */}
          <button
            onClick={() => setShowRoster((v) => !v)}
            className="mt-3 text-[11px] text-muted underline-offset-2 hover:text-ink hover:underline"
          >
            {showRoster ? "Hide" : "Show"} what the review board is given
          </button>
          {showRoster && (
            <pre className="mt-2 overflow-x-auto whitespace-pre-wrap rounded border border-edge bg-void px-3 py-2 text-[11px] leading-relaxed text-muted">
              {lib.roster || "(no roles switched on — the board will invent a team)"}
            </pre>
          )}
        </>
      )}
    </section>
  );
}
