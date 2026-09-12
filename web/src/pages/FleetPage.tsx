import { useCallback, useEffect, useState } from "react";
import { api } from "../lib/api";
import { RoleLibrarySection } from "../components/RoleLibrary";
import type { BotSummary, Fleet, FleetMember } from "../lib/types";

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

/** Start tuning a bot that hasn't been tuned yet.
 *
 * This is the entrance the Fleet never had. The page listed only bots whose
 * instructions you had already changed, and told you to change one "on its
 * card in the Bot library" — where no such control exists, and which is
 * hidden entirely in Basic mode. So the one documented way to shape how a
 * bot works was unreachable from the UI: a closed loop with no way in.
 *
 * The list stays what you've tuned rather than the whole catalog — that is
 * the point of a Fleet — so adding someone is a deliberate act, here. */
function TuneAnother({
  tuned,
  onChanged,
}: {
  tuned: Set<string>;
  onChanged: () => void;
}) {
  const [all, setAll] = useState<BotSummary[] | null>(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open && all === null) api.listBots().then(setAll).catch((e: unknown) => setError(String(e)));
  }, [open, all]);

  // Only bots that actually take instructions — the rest have nothing to
  // tune, and listing them would make the picker a bot catalog.
  const tunable = (all ?? []).filter(
    (b) => !tuned.has(b.id) && b.inputs.some((p) => p.name === "instructions"),
  );

  const start = async (b: BotSummary) => {
    setBusy(b.id);
    setError(null);
    try {
      // Seed with what it ships with, so the first edit is a change to
      // something real rather than typing into an empty box.
      const shipped = b.inputs.find((p) => p.name === "instructions")?.default ?? "";
      await api.setBotInstructions(b.id, shipped);
      onChanged();
      setOpen(false);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(null);
    }
  };

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        className="mt-3 rounded-lg border border-dashed border-edge-strong px-4 py-2.5 text-[13px] text-muted transition-colors hover:border-tron hover:text-ink"
      >
        + Tune how a bot works
      </button>
    );
  }

  return (
    <div className="mt-3 rounded-lg border border-tron/60 bg-panel p-4">
      <div className="flex items-baseline justify-between gap-2">
        <span className="font-display text-sm text-ink">Which bot?</span>
        <button onClick={() => setOpen(false)} className="text-[11px] text-muted hover:text-ink">
          Cancel
        </button>
      </div>
      {all === null && !error && <p className="mt-2 text-[13px] text-muted/60">Loading…</p>}
      {error && <p className="mt-2 text-[12px] text-danger">{error}</p>}
      {all !== null && tunable.length === 0 && (
        <p className="mt-2 text-[13px] text-muted">
          Every bot that takes instructions is already in your fleet.
        </p>
      )}
      <div className="mt-2 grid max-h-72 grid-cols-1 gap-1 overflow-auto sm:grid-cols-2">
        {tunable.map((b) => (
          <button
            key={b.id}
            onClick={() => void start(b)}
            disabled={busy !== null}
            className="rounded border border-edge px-2.5 py-1.5 text-left transition-colors hover:border-tron disabled:opacity-40"
          >
            <span className="font-display text-[13px] text-ink">{b.id}</span>
            <span className="ml-1.5 text-[11px] text-muted">{b.description}</span>
          </button>
        ))}
      </div>
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
      <p className="mt-1 hidden max-w-2xl text-sm text-muted sm:block">
        How your team works — two separate things. Below: the bots you've told
        how to do their job, each showing what it shipped with so you can put
        it back. Further down: the perspectives a review board draws on, which
        aren't bots and aren't assigned to them.
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
          <p className="text-sm text-ink">You haven't told anyone how to work yet.</p>
          <p className="mt-1.5 text-[13px] leading-snug text-muted">
            Every bot with an LLM step ships with a suggested way of working —
            "Anything mentioning data loss or billing is top priority". Pick one
            below to change it, and it'll appear here alongside what it started
            with, so you can always put it back.
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

      <TuneAnother
        tuned={new Set((fleet?.members ?? []).map((m) => m.bot_id))}
        onChanged={reload}
      />

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
