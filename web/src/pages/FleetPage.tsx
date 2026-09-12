import { useCallback, useEffect, useState } from "react";
import { api } from "../lib/api";
import { RoleLibrarySection } from "../components/RoleLibrary";
import type { Fleet, FleetMember } from "../lib/types";

/**
 * The Fleet answers a different question from the Bot library.
 *
 * The library is the catalog: every bot that exists, so you can see what
 * snaps into what. The Fleet is "who works for me, and how have I told them
 * to behave" — only the bots whose instructions you've actually changed,
 * and the teams they work in. A fresh install has an empty Fleet, which is
 * correct: you haven't told anyone anything yet.
 */
function MemberRow({ member, onChanged }: { member: FleetMember; onChanged: () => void }) {
  const [text, setText] = useState(member.instructions);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dirty = text.trim() !== member.instructions.trim();

  const save = async (value: string) => {
    setBusy(true);
    setError(null);
    try {
      await api.setBotInstructions(member.bot_id, value);
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="rounded-lg border border-edge-strong bg-panel p-4">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <span className="font-display text-sm font-semibold text-ink">{member.bot_id}</span>
        {member.used_in.length > 0 ? (
          <span className="text-[11px] text-muted">works in {member.used_in.join(", ")}</span>
        ) : (
          <span className="text-[11px] text-muted/60">not in any swarm yet</span>
        )}
      </div>

      <textarea
        value={text}
        onChange={(e) => setText(e.target.value.replace(/\n/g, " "))}
        rows={2}
        maxLength={2000}
        className="mt-2 w-full resize-none rounded border border-edge bg-void px-2.5 py-1.5 text-[13px] leading-snug text-ink focus:border-tron focus:outline-none"
      />

      <div className="mt-1.5 flex flex-wrap items-center gap-2 text-[11px]">
        <button
          onClick={() => void save(text)}
          disabled={busy || !dirty}
          className="rounded border border-edge-strong px-2 py-0.5 text-ink transition-colors hover:border-tron disabled:opacity-40"
        >
          {busy ? "Saving…" : "Save"}
        </button>
        {/* Putting it back is what makes tuning safe to experiment with —
            and it's only possible because the shipped value is recorded
            before the first edit overwrites it. */}
        <button
          onClick={() => {
            setText(member.shipped);
            void save(member.shipped);
          }}
          disabled={busy}
          className="rounded border border-edge px-2 py-0.5 text-muted transition-colors hover:border-warn hover:text-warn disabled:opacity-40"
          title={member.shipped || "(it shipped with nothing set)"}
        >
          Put back what it shipped with
        </button>
        {error && <span className="text-danger">{error}</span>}
      </div>

      <p className="mt-1.5 text-[11px] leading-snug text-muted/70">
        <span className="text-muted">shipped with · </span>
        {member.shipped || <span className="italic">nothing set</span>}
      </p>
    </div>
  );
}

export function FleetPage() {
  const [fleet, setFleet] = useState<Fleet | null>(null);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .fleet()
      .then(setFleet)
      .catch((e) => setError(String(e)));
  }, []);
  useEffect(reload, [reload]);

  const teamsWithTuned = (fleet?.teams ?? []).filter((t) => t.tuned.length > 0);

  return (
    <div className="h-full overflow-auto p-5 sm:p-6">
      <h1 className="font-display text-xl font-medium text-ink">Fleet</h1>
      <p className="mt-1 hidden text-sm text-muted sm:block">
        The bots you've told how to work, and the teams they work in. Every
        bot ships with a suggestion; this is where the ones you've changed
        live, so you can see what you asked for and put any of it back.
      </p>

      {error && (
        <div className="mt-6 rounded border border-danger/40 bg-danger/5 px-4 py-3 text-sm text-danger">
          Couldn't load the fleet: {error}
        </div>
      )}

      {fleet === null && !error && (
        <p className="mt-5 text-sm text-muted/60">Loading…</p>
      )}

      {fleet !== null && fleet.members.length === 0 && (
        <div className="mt-6 max-w-xl rounded-lg border border-edge bg-panel/40 p-5">
          <p className="text-sm text-ink">Nobody's been tuned yet.</p>
          <p className="mt-1.5 text-[13px] leading-snug text-muted">
            Every bot with an LLM step ships with a suggested way of working —
            "Anything mentioning data loss or billing is top priority". Change
            one on its card in the Bot library and it'll appear here, with what
            it started from.
          </p>
        </div>
      )}

      {fleet !== null && fleet.members.length > 0 && (
        <div className="mt-5 grid grid-cols-1 gap-3 lg:grid-cols-2">
          {fleet.members.map((m) => (
            <MemberRow key={m.bot_id} member={m} onChanged={reload} />
          ))}
        </div>
      )}

      <RoleLibrarySection />

      {teamsWithTuned.length > 0 && (
        <section className="mt-8">
          <h2 className="font-display text-xs font-semibold uppercase tracking-wider text-muted">
            Teams with someone you've tuned
          </h2>
          <div className="mt-2 flex flex-col gap-1.5">
            {teamsWithTuned.map((t) => (
              <div
                key={t.path}
                className="rounded-lg border border-edge bg-panel/40 px-4 py-2.5 text-[13px]"
              >
                <span className="font-display text-ink">{t.swarm}</span>
                <span className="ml-2 text-[11px] text-muted">
                  {t.members.map((m) => (
                    <span key={m} className={t.tuned.includes(m) ? "text-tron" : undefined}>
                      {m}{" "}
                    </span>
                  ))}
                </span>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}
