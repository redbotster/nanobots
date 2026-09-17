import { useCallback, useEffect, useState } from "react";
import { api } from "../lib/api";
import { listBotsCached } from "../lib/botsCache";
import { InstructionsEditor } from "../components/BotCard";
import { RoleLibrarySection } from "../components/RoleLibrary";
import { Tabs } from "../components/Tabs";
import type { BotSummary, Team, TeamMember } from "../lib/types";

/**
 * The Team answers a different question from the Bot library.
 *
 * The library is the catalog: every bot that exists, so you can see what
 * snaps into what. The Team is "who works for me, and how have I told them
 * to behave" — only the bots whose instructions you've actually changed,
 * and where that takes effect. A fresh install has an empty one, which is
 * correct: you haven't told anyone anything yet.
 */
function MemberRow({ member, onChanged }: { member: TeamMember; onChanged: () => void }) {
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
 * This is the entrance this page never had. The page listed only bots whose
 * instructions you had already changed, and told you to change one "on its
 * card in the Bot library" — where no such control exists, and which is
 * hidden entirely in Basic mode. So the one documented way to shape how a
 * bot works was unreachable from the UI: a closed loop with no way in.
 *
 * The list stays what you've tuned rather than the whole catalog — that is
 * the point of this page — so adding someone is a deliberate act, here. */
export function TuneAnother({ tuned, onChanged }: { tuned: Set<string>; onChanged: () => void }) {
  const [all, setAll] = useState<BotSummary[] | null>(null);
  const [open, setOpen] = useState(false);
  const [drafting, setDrafting] = useState<BotSummary | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open && all === null)
      listBotsCached()
        .then(setAll)
        .catch((e: unknown) => setError(String(e)));
  }, [open, all]);

  // Only bots that actually take instructions — the rest have nothing to
  // tune, and listing them would make the picker a bot catalog.
  const tunable = (all ?? []).filter(
    (b) => !tuned.has(b.id) && b.inputs.some((p) => p.name === "instructions"),
  );

  // Picking a bot opens its editor. It does not save anything yet.
  //
  // This used to POST the bot's own shipped instructions straight back,
  // unchanged, to get it into the team — and the server records a bot only
  // when the text actually differs from what is already there, which for an
  // unchanged value it never does. So the API returned 200, nothing was
  // recorded, the picker closed, and the page still said "Nothing tuned
  // yet". Picking a bot did nothing at all, silently.
  //
  // The page's own copy already says what should happen: "changing one
  // brings it here". So choose a bot, see what it ships with, change it,
  // save — and that save is what puts it in the team.
  const start = (b: BotSummary) => {
    setError(null);
    setDrafting(b);
    setOpen(false);
  };

  if (drafting) {
    const shipped = drafting.inputs.find((p) => p.name === "instructions")?.default ?? "";
    return (
      <div className="mt-3 rounded-lg border border-tron/60 bg-panel p-4">
        <div className="flex items-baseline justify-between gap-2">
          <span className="font-display text-sm text-ink">{drafting.id}</span>
          <span className="text-[11px] text-muted">
            change this and save to add it to your team
          </span>
        </div>
        <InstructionsEditor
          botId={drafting.id}
          current={shipped}
          startOpen
          onCancel={() => setDrafting(null)}
          onChanged={() => {
            setDrafting(null);
            onChanged();
          }}
        />
      </div>
    );
  }

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
          Every bot that takes instructions is already in your team.
        </p>
      )}
      <div className="mt-2 grid max-h-72 grid-cols-1 gap-1 overflow-auto sm:grid-cols-2">
        {tunable.map((b) => (
          <button
            key={b.id}
            onClick={() => start(b)}
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

export function TeamPage() {
  const [team, setTeam] = useState<Team | null>(null);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .team()
      .then(setTeam)
      .catch((e) => setError(String(e)));
  }, []);
  useEffect(reload, [reload]);

  // Two tabs, not one page with a paragraph explaining that it is really
  // two things. The old header opened "How your team works — two separate
  // things. Below: … Further down: …", which is a page apologising for its
  // own structure: about twelve lines of prose before the first control.
  // Bot instructions and review roles are genuinely unrelated — one tunes a
  // bot you own, the other edits perspectives a review board draws on — so
  // they get somewhere to be unrelated.
  return (
    <div className="flex h-full flex-col p-5 sm:p-6">
      <h1 className="font-display text-xl font-medium text-ink">Team</h1>

      <div className="mt-3 min-h-0 flex-1">
        <Tabs
          defaultValue="bots"
          tabs={[
            {
              value: "bots",
              label: "How bots work",
              content: (
                <div className="h-full overflow-auto pt-4">
                  <p className="max-w-2xl text-[13px] leading-snug text-muted">
                    The bots you've told how to do their job. Each keeps what it shipped with, so
                    you can always put it back.
                  </p>

                  {error && (
                    <div className="mt-4 rounded border border-danger/40 bg-danger/5 px-4 py-3 text-sm text-danger">
                      Couldn't load the team: {error}
                    </div>
                  )}
                  {team === null && !error && (
                    <p className="mt-4 text-sm text-muted/60">Loading…</p>
                  )}
                  {team !== null && team.members.length === 0 && (
                    <p className="mt-4 max-w-xl text-[13px] leading-snug text-muted">
                      Nothing tuned yet. Every bot with an LLM step ships with a suggested way of
                      working — "Anything mentioning data loss or billing is top priority" — and
                      changing one brings it here.
                    </p>
                  )}
                  {team !== null && team.members.length > 0 && (
                    <div className="mt-4 grid grid-cols-1 gap-3 lg:grid-cols-2">
                      {team.members.map((m) => (
                        <MemberRow key={m.bot_id} member={m} onChanged={reload} />
                      ))}
                    </div>
                  )}

                  <TuneAnother
                    tuned={new Set((team?.members ?? []).map((m) => m.bot_id))}
                    onChanged={reload}
                  />
                </div>
              ),
            },
            {
              value: "roles",
              label: "Review roles",
              content: (
                <div className="h-full overflow-auto pt-4">
                  <RoleLibrarySection />
                </div>
              ),
            },
          ]}
        />
      </div>
    </div>
  );
}
