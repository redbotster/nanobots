import { useEffect, useMemo, useState, useRef } from "react";
import { SchedulePicker } from "../components/builder/SchedulePicker";
import { api } from "../lib/api";
import { listBotsCached } from "../lib/botsCache";
import { listSwarmsCached } from "../lib/swarmsCache";
import type {
  BotSummary,
  ConnectionStatus,
  DraftBot,
  DraftSnap,
  PlanResult,
  SaveSwarmRequest,
  SwarmSummary,
} from "../lib/types";
import {
  BuilderCanvas,
  type CanvasSnap,
  type PlacedBot,
} from "../components/builder/BuilderCanvas";
import { placeBots, toCanvasSnaps, toDraftBot } from "../components/builder/hydrate";
import { BuilderPalette } from "../components/builder/BuilderPalette";
import { BuilderInspector } from "../components/builder/BuilderInspector";
import { Button } from "../components/Button";

function uniqueInstanceId(base: string, existing: Set<string>): string {
  if (!existing.has(base)) return base;
  for (let i = 2; ; i++) {
    const candidate = `${base}-${i}`;
    if (!existing.has(candidate)) return candidate;
  }
}

// Column/row gutters are wider than the 208px node itself — each node's
// port labels hang outside its own border (see BuilderCanvas's translated
// port dots), so two nodes placed only a node-width apart visually collide.
function defaultPosition(index: number) {
  return { x: 40 + (index % 3) * 360, y: 40 + Math.floor(index / 3) * 220 };
}

/** The first grid slot nothing already occupies.
 *
 * Placing by bots.length meant that after any delete the next bot landed
 * exactly on top of an existing one — add A, add B, delete A, add C, and B
 * and C sit at identical coordinates, fully stacked. The palette click looks
 * like it did nothing, or like it replaced the node you could see, while the
 * hidden bot is still in the swarm and still gets saved. */
function freePosition(taken: { x: number; y: number }[]) {
  for (let i = 0; i < 200; i++) {
    const pos = defaultPosition(i);
    if (!taken.some((b) => b.x === pos.x && b.y === pos.y)) return pos;
  }
  return defaultPosition(taken.length);
}

/** The visual swarm builder: place bots from the catalog onto a canvas,
 * drag connections between typed ports, and save the result as a real
 * nanoswarm.yaml — see internal/api/builder.go for the backend half.
 *
 * Known scope cuts, disclosed rather than hidden: connections are always
 * whole-port-to-port (no picking a nested field of a json-typed output, the
 * way e.g. daily-email-recap.yaml's `recap.recap_json.headline` snap does —
 * that still requires hand-editing YAML); a bot's manual input values are
 * always plain strings, not a per-type editor; trigger/vars/deploy stay at
 * their manual-trigger/local-target defaults, with no UI for them yet; and
 * node layout lives only in this component's state, not persisted anywhere,
 * so it resets on reload (the saved swarm.yaml itself has no layout concept
 * to persist it into).
 */
export function BuilderPage({
  existing,
  composedDraft,
  onDone,
}: {
  existing?: SwarmSummary;
  /** A draft from the AI composer (POST /api/compose) to pre-load — same
   * treatment as a template, minus the "copy" suffix, since this is the
   * user's own first draft, not a clone of something else. */
  composedDraft?: SaveSwarmRequest;
  onDone: (savedPath?: string) => void;
}) {
  const [botDefs, setBotDefs] = useState<Record<string, BotSummary>>({});
  const [connections, setConnections] = useState<ConnectionStatus[]>([]);
  const [name, setName] = useState(existing?.name ?? composedDraft?.name ?? "");
  const [description, setDescription] = useState(
    existing?.description ?? composedDraft?.description ?? "",
  );
  // Only tracked for a *new* swarm. Editing an existing one leaves its
  // trigger alone — the builder does not model triggers, and sending a
  // value here on every save would unschedule anything you opened.
  const [schedule, setSchedule] = useState(composedDraft?.schedule ?? "");
  const [bots, setBots] = useState<PlacedBot[]>([]);
  const [snaps, setSnaps] = useState<CanvasSnap[]>([]);
  const [inputValues, setInputValues] = useState<Record<string, Record<string, string>>>({});
  const [selected, setSelected] = useState<string | null>(null);
  const [validation, setValidation] = useState<PlanResult | null>(null);
  const [validating, setValidating] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [templates, setTemplates] = useState<SwarmSummary[]>([]);
  // A ref, not state: it only ever gates the leave guard, and flipping it
  // shouldn't cause a render in the middle of navigating away.
  const savedRef = useRef(false);
  const [paletteOpen, setPaletteOpen] = useState(false);

  // The catalog and the connection list do not depend on which swarm is
  // being edited, so they load once.
  useEffect(() => {
    listBotsCached().then((list) => {
      setBotDefs(Object.fromEntries(list.map((b) => [b.id, b])));
    });
    api
      .listConnections()
      .then(setConnections)
      .catch(() => {});
  }, []);

  // Templates are only offered when starting from nothing, so this one does
  // depend on `existing` — and is split out rather than folded above so
  // saying so does not also refetch the whole bot catalog. Keyed on the
  // path rather than the object: the swarm list is polled, so its objects
  // can be new on a tick that changed a different swarm entirely.
  const existingPath = existing?.path;
  useEffect(() => {
    if (existingPath) return;
    listSwarmsCached()
      .then(({ swarms }) => setTemplates(swarms))
      .catch(() => {});
  }, [existingPath]);

  // Places a loaded swarm's bots in a simple grid and pre-fills manual input
  // values — shared by hydrating an existing swarm to edit, cloning one as a
  // starting template, and loading the AI composer's draft, since all three
  // are just "a bots[]/snaps[] list to put on the canvas."
  const hydrateFrom = (full: { bots: DraftBot[]; snaps: DraftSnap[] }) => {
    // The mapping lives in ./builder/hydrate so it can be tested: the
    // builder round-trips a whole swarm on save, so anything it forgets to
    // load, it deletes.
    const placed = placeBots(full.bots, defaultPosition);
    setBots(placed);
    setSnaps(toCanvasSnaps(full.snaps));
    setInputValues(
      Object.fromEntries(
        full.bots.map((b) => [
          b.id,
          Object.fromEntries(Object.entries(b.inputs ?? {}).map(([k, v]) => [k, String(v)])),
        ]),
      ),
    );
  };

  // Hydrate from an existing swarm's structured data (edit mode) once its
  // definition is loaded, so we know each placed bot's directory id (the
  // "use:" field only carries "<dir-id>@<version>", already what we need).
  useEffect(() => {
    if (!existing) return;
    api
      .swarmFull(existing.path)
      .then(hydrateFrom)
      .catch((e) => setLoadError(String(e)));
  }, [existing]);

  // The AI composer already validated this draft server-side (see
  // internal/api/compose.go) — hydrating it here just puts it on the
  // canvas for review; nothing is saved or run until the human does that
  // themselves.
  useEffect(() => {
    if (!composedDraft) return;
    hydrateFrom(composedDraft);
  }, [composedDraft]);

  const startFromTemplate = async (template: SwarmSummary) => {
    try {
      const full = await api.swarmFull(template.path);
      hydrateFrom(full);
      setName(`${template.name} copy`);
      setDescription(template.description);
    } catch (e) {
      setLoadError(String(e));
    }
  };

  // Live, debounced type-checking against the real planner — the same rules
  // `nanobots plan` and a saved swarm's run both use — so a bad connection
  // shows up red before it's ever written to disk.
  useEffect(() => {
    if (bots.length === 0) {
      setValidation(null);
      return;
    }
    setValidating(true);
    const t = setTimeout(() => {
      api
        .validateSwarm({
          bots: bots.map((b) => toDraftBot(b, botDefs, inputValues[b.instanceId])),
          snaps,
        })
        .then(setValidation)
        .catch((e) =>
          setValidation({ swarm: "draft", bots: [], snaps: [], ok: false, error: String(e) }),
        )
        .finally(() => setValidating(false));
    }, 350);
    return () => clearTimeout(t);
  }, [bots, snaps, inputValues, botDefs]);

  const addBot = (bot: BotSummary) => {
    const id = uniqueInstanceId(bot.id, new Set(bots.map((b) => b.instanceId)));
    const pos = freePosition(bots);
    setBots((prev) => [...prev, { instanceId: id, botId: bot.id, x: pos.x, y: pos.y }]);
  };

  const removeBot = (instanceId: string) => {
    setBots((prev) => prev.filter((b) => b.instanceId !== instanceId));
    setSnaps((prev) =>
      prev.filter(
        (s) => !s.from.startsWith(instanceId + ".") && !s.to.startsWith(instanceId + "."),
      ),
    );
    setInputValues((prev) => {
      const { [instanceId]: _drop, ...rest } = prev;
      return rest;
    });
    if (selected === instanceId) setSelected(null);
  };

  const addSnap = (snap: CanvasSnap) => {
    setSnaps((prev) => {
      // One connection per input port — a fresh one to the same target
      // replaces whatever fed it before, rather than silently stacking.
      const withoutTarget = prev.filter((s) => s.to !== snap.to);
      return [...withoutTarget, snap];
    });
  };

  const removeSnap = (index: number) => setSnaps((prev) => prev.filter((_, i) => i !== index));

  // Lets a human pick a nested field of a json/list<json> output from the
  // Inspector (e.g. "recap.recap_json" -> "recap.recap_json.headline")
  // instead of only ever being possible by hand-editing the saved YAML —
  // the canvas itself still only draws whole-port-to-port lines (see
  // BuilderCanvas's own doc comment), so this is deliberately a text
  // suffix on the existing connection, not a new visual affordance. A bad
  // field name surfaces exactly the way any other type mismatch already
  // does: a real validation error from the same planner a save goes
  // through, not a silent acceptance.
  const editSnapFrom = (index: number, newFrom: string) =>
    setSnaps((prev) => prev.map((s, i) => (i === index ? { ...s, from: newFrom } : s)));

  const editSnapJoin = (index: number, join: string) =>
    setSnaps((prev) => prev.map((s, i) => (i === index ? { ...s, join: join || undefined } : s)));

  const editBotOnError = (instanceId: string, onError: string) =>
    setBots((prev) =>
      prev.map((b) => (b.instanceId === instanceId ? { ...b, onError: onError || undefined } : b)),
    );

  const selectedBot = bots.find((b) => b.instanceId === selected) ?? null;
  const selectedDef = selectedBot ? botDefs[selectedBot.botId] : null;

  const snapChecksByPair = useMemo(() => validation?.snaps ?? [], [validation]);

  const save = async () => {
    setSaving(true);
    setSaveError(null);
    try {
      const result = await api.saveSwarm({
        path: existing?.path,
        name,
        description,
        bots: bots.map((b) =>
          toDraftBot(
            b,
            botDefs,
            Object.fromEntries(
              Object.entries(inputValues[b.instanceId] ?? {}).filter(([, v]) => v.trim() !== ""),
            ),
          ),
        ),
        snaps,
        // Only on create. undefined on an edit is what tells the server to
        // keep the existing trigger.
        schedule: existing ? undefined : schedule,
      });
      savedRef.current = true; // a successful save is not unsaved work
      onDone(result.path);
    } catch (e) {
      setSaveError(String(e));
    } finally {
      setSaving(false);
    }
  };

  const canSave = name.trim() !== "" && bots.length > 0 && !saving;

  // Node layout isn't persisted and there's no draft storage, so leaving is
  // genuinely destructive — a ten-minute build was one stray click from
  // nothing, with no dialog and no undo. "Dirty" is deliberately generous:
  // anything placed at all counts, since a swarm with bots on the canvas is
  // work worth protecting whether or not it's been named yet.
  const dirty = bots.length > 0 && !savedRef.current;

  const leave = () => {
    if (
      dirty &&
      !window.confirm("Leave the builder? Your unsaved changes to this swarm will be lost.")
    ) {
      return;
    }
    onDone();
  };

  // Covers reload and tab-close, which the in-app guard can't see.
  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  return (
    <div className="grid h-full grid-rows-[auto_1fr]">
      <header className="flex flex-wrap items-center gap-3 border-b border-edge px-4 py-3 sm:px-6">
        <button onClick={leave} className="font-display text-xs text-muted hover:text-ink">
          ← Swarms
        </button>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Swarm name"
          className="min-w-[140px] flex-1 rounded border border-edge-strong bg-void px-2.5 py-1.5 font-display text-sm text-ink placeholder:text-muted focus:border-tron focus:outline-none sm:flex-none sm:w-52"
        />
        <input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Description (optional)"
          className="min-w-[180px] flex-[2] rounded border border-edge-strong bg-void px-2.5 py-1.5 text-sm text-ink placeholder:text-muted focus:border-tron focus:outline-none"
        />

        {/* Only when creating: an edit leaves the trigger alone, so a
            control here would be a promise the save does not keep. */}
        {!existing && <SchedulePicker value={schedule} onChange={setSchedule} />}

        <div className="ml-auto flex items-center gap-3">
          {validating && <span className="text-xs text-muted">checking…</span>}
          {!validating && validation && (
            <span className={`text-xs ${validation.ok ? "text-ok" : "text-warn"}`}>
              {validation.ok
                ? `${bots.length} bot${bots.length === 1 ? "" : "s"} · ready to run`
                : validation.error
                  ? validation.error
                  : `${validation.snaps.filter((s) => !s.OK).length} connection(s) need fixing`}
            </span>
          )}
          {saveError && <span className="text-xs text-danger">{saveError}</span>}
          <Button variant="primary" onClick={save} disabled={!canSave}>
            {saving ? "Saving…" : existing ? "Save changes" : "Save swarm"}
          </Button>
        </div>
      </header>

      {loadError && (
        <div className="border-b border-danger/40 bg-danger/5 px-6 py-2 text-sm text-danger">
          Couldn't load this swarm for editing: {loadError}
        </div>
      )}

      {/* Below sm the palette (220px) and inspector (256px) together
          exceed a 390px viewport, which collapsed the canvas to zero width
          and pushed the inspector's close button off-screen with no
          horizontal scroll to reach it — selecting a bot was a dead end.
          Both become overlays there; the canvas keeps the full width. */}
      <div className="relative grid min-h-0 grid-rows-[minmax(0,1fr)] grid-cols-1 sm:grid-cols-[240px_1fr]">
        <div className="hidden min-h-0 sm:block">
          <BuilderPalette bots={Object.values(botDefs)} connections={connections} onAdd={addBot} />
        </div>

        {paletteOpen && (
          <div className="absolute inset-0 z-30 flex sm:hidden">
            <div className="flex min-h-0 w-[85%] max-w-xs flex-col bg-void shadow-glow">
              <div className="flex items-center justify-between border-b border-edge px-3 py-2">
                <span className="font-display text-xs font-semibold text-ink">Add a bot</span>
                <button
                  onClick={() => setPaletteOpen(false)}
                  className="rounded p-1 text-muted hover:text-ink"
                  aria-label="Close the bot palette"
                >
                  ✕
                </button>
              </div>
              <div className="min-h-0 flex-1">
                <BuilderPalette
                  bots={Object.values(botDefs)}
                  connections={connections}
                  onAdd={(b) => {
                    addBot(b);
                    setPaletteOpen(false);
                  }}
                />
              </div>
            </div>
            <button
              className="flex-1 bg-void/60"
              onClick={() => setPaletteOpen(false)}
              aria-label="Close the bot palette"
            />
          </div>
        )}

        <div className="flex min-h-0 min-w-0">
          <div className="relative min-w-0 flex-1">
            {bots.length === 0 && !existing && templates.length > 0 && (
              <div className="pointer-events-none absolute inset-x-0 bottom-6 z-10 flex justify-center">
                <div className="pointer-events-auto flex max-w-lg flex-wrap items-center gap-2 rounded-lg border border-edge-strong bg-panel/95 px-4 py-3 shadow-glow-sm backdrop-blur">
                  <span className="text-xs text-muted">Or start from a template:</span>
                  {templates.map((t) => (
                    <button
                      key={t.path}
                      onClick={() => startFromTemplate(t)}
                      className="rounded-full border border-edge-strong px-3 py-1 text-xs text-ink transition-colors hover:border-tron hover:bg-tron/10"
                    >
                      {t.name}
                    </button>
                  ))}
                </div>
              </div>
            )}
            <BuilderCanvas
              bots={bots}
              botDefs={botDefs}
              snaps={snaps}
              snapChecks={snapChecksByPair}
              onMoveBot={(id, x, y) =>
                setBots((prev) => prev.map((b) => (b.instanceId === id ? { ...b, x, y } : b)))
              }
              onRemoveBot={removeBot}
              onAddSnap={addSnap}
              onRemoveSnap={removeSnap}
              selectedInstanceId={selected}
              onSelectBot={setSelected}
            />
          </div>
          {/* A phone gets the inspector as a bottom sheet — full width, its
              close button always on screen. */}
          {selectedBot && selectedDef && (
            <div className="absolute inset-x-0 bottom-0 z-30 max-h-[70%] border-t border-edge-strong bg-void shadow-glow sm:static sm:z-auto sm:max-h-none sm:border-t-0 sm:shadow-none">
              <BuilderInspector
                bot={selectedBot}
                def={selectedDef}
                snaps={snaps}
                values={inputValues[selectedBot.instanceId] ?? {}}
                onChange={(port, value) =>
                  setInputValues((prev) => ({
                    ...prev,
                    [selectedBot.instanceId]: { ...prev[selectedBot.instanceId], [port]: value },
                  }))
                }
                onEditSnapFrom={editSnapFrom}
                onEditSnapJoin={editSnapJoin}
                onEditOnError={(v) => editBotOnError(selectedBot.instanceId, v)}
                onClose={() => setSelected(null)}
              />
            </div>
          )}

          {/* The only way to reach the palette on a phone. */}
          <button
            onClick={() => setPaletteOpen(true)}
            className="absolute bottom-4 right-4 z-20 rounded-full border border-edge-strong bg-panel px-4 py-2.5 font-display text-sm text-ink shadow-glow-sm sm:hidden"
          >
            + Add a bot
          </button>
        </div>
      </div>
    </div>
  );
}
