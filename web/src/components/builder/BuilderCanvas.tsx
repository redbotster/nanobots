import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { BotSummary, SnapCheck } from "../../lib/types";

export interface PlacedBot {
  instanceId: string;
  botId: string;
  x: number;
  y: number;
}

export interface CanvasSnap {
  from: string; // "<instanceId>.<port>"
  to: string;
}

const NODE_WIDTH = 208;
const ROW_HEIGHT = 24;
const HEADER_HEIGHT = 40;

type Point = { x: number; y: number };

/** Cheap equality for the measured port map — sub-pixel jitter from a
 * scrollbar appearing shouldn't count as movement and re-render the canvas. */
function samePositions(a: Record<string, Point>, b: Record<string, Point>) {
  const keys = Object.keys(a);
  if (keys.length !== Object.keys(b).length) return false;
  for (const k of keys) {
    const p = a[k];
    const q = b[k];
    if (!q || Math.abs(p.x - q.x) > 0.5 || Math.abs(p.y - q.y) > 0.5) return false;
  }
  return true;
}

function portKey(instanceId: string, dir: "in" | "out", port: string) {
  return `${instanceId}:${dir}:${port}`;
}

// A snap endpoint is "<instanceId>.<port>", possibly followed by more
// dotted segments selecting a nested field (e.g.
// "recap.recap_json.headline") — the builder only draws whole-port-to-port
// connections (a known, disclosed scope cut; see BuilderPage's doc comment),
// so only the first two segments matter for finding the port to draw a line
// from/to.
function endpointPort(ref: string): { instanceId: string; port: string } {
  const [instanceId, port] = ref.split(".");
  return { instanceId, port };
}

/** The visual swarm builder's canvas: draggable bot nodes with typed ports,
 * and click-drag-release connections between an output port and an input
 * port. Node positions are canvas-local only — the swarm's saved YAML has no
 * concept of layout, so this state lives only in BuilderPage's component
 * state and resets on reload (a disclosed limitation, see its doc comment).
 * Delete/Backspace removes the selected node when focus isn't in a text
 * field; Escape cancels an in-progress connection drag. */
export function BuilderCanvas({
  bots,
  botDefs,
  snaps,
  snapChecks,
  onMoveBot,
  onRemoveBot,
  onAddSnap,
  onRemoveSnap,
  selectedInstanceId,
  onSelectBot,
}: {
  bots: PlacedBot[];
  botDefs: Record<string, BotSummary>;
  snaps: CanvasSnap[];
  snapChecks: SnapCheck[];
  onMoveBot: (instanceId: string, x: number, y: number) => void;
  onRemoveBot: (instanceId: string) => void;
  onAddSnap: (snap: CanvasSnap) => void;
  onRemoveSnap: (index: number) => void;
  selectedInstanceId: string | null;
  onSelectBot: (instanceId: string) => void;
}) {
  const canvasRef = useRef<HTMLDivElement>(null);
  const portEls = useRef(new Map<string, HTMLElement>());
  const [positions, setPositions] = useState<Record<string, { x: number; y: number }>>({});
  const [connecting, setConnecting] = useState<{ instanceId: string; port: string } | null>(null);
  const [cursor, setCursor] = useState<{ x: number; y: number } | null>(null);

  const registerPort = (key: string) => (el: HTMLElement | null) => {
    if (el) portEls.current.set(key, el);
    else portEls.current.delete(key);
  };

  // Recomputes every port's canvas-relative pixel center from the live DOM —
  // getBoundingClientRect is viewport-relative for both the port and the
  // canvas, so their difference is correct regardless of scroll position, no
  // manual scroll bookkeeping needed.
  //
  // This runs after *every* render rather than on a dependency list, and it
  // is not laziness. It used to depend on [bots, snaps], which silently
  // failed on the most common path there is: opening an existing swarm to
  // edit it. Ports only render once botDefs has loaded over the network, so
  // the effect measured an empty canvas, botDefs then arrived without
  // changing bots or snaps, and the effect never ran again — a five-bot
  // swarm opened in the builder with all of its connections invisible. Any
  // dependency list here is a guess about what makes ports move; measuring
  // unconditionally and writing only on a real change is not a guess.
  const measure = () => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const canvasBox = canvas.getBoundingClientRect();
    const next: Record<string, { x: number; y: number }> = {};
    for (const [key, el] of portEls.current) {
      const box = el.getBoundingClientRect();
      next[key] = {
        x: box.left + box.width / 2 - canvasBox.left + canvas.scrollLeft,
        y: box.top + box.height / 2 - canvasBox.top + canvas.scrollTop,
      };
    }
    // Bail unless something actually moved — setPositions on every render
    // would loop forever.
    setPositions((prev) => (samePositions(prev, next) ? prev : next));
  };

  useLayoutEffect(measure);

  // A canvas resize moves every port without re-rendering React, so the
  // effect above would never see it — the connectors would detach from the
  // ports until the next unrelated render.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => measure());
    ro.observe(canvas);
    return () => ro.disconnect();
  }, []);

  // Delete/Backspace removes the selected node — skipped while focus is in
  // a text field (the swarm name/description inputs, a palette search box)
  // so it still behaves like a normal delete key there. Escape backs out of
  // an in-progress connection drag without requiring a precise mouseup.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      const inTextField = target && ["INPUT", "TEXTAREA"].includes(target.tagName);
      if ((e.key === "Delete" || e.key === "Backspace") && !inTextField && selectedInstanceId) {
        e.preventDefault();
        onRemoveBot(selectedInstanceId);
      }
      if (e.key === "Escape" && connecting) {
        cancelConnectionRef.current?.();
        setConnecting(null);
        setCursor(null);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [selectedInstanceId, connecting, onRemoveBot]);

  const startDragNode = (instanceId: string, e: React.MouseEvent) => {
    if ((e.target as HTMLElement).closest("[data-port]")) return;
    e.preventDefault();
    onSelectBot(instanceId);
    const bot = bots.find((b) => b.instanceId === instanceId);
    if (!bot) return;
    const startX = e.clientX;
    const startY = e.clientY;
    const originX = bot.x;
    const originY = bot.y;
    const onMove = (ev: MouseEvent) => {
      // Clamped at the origin: dragging a node up and left past 0,0 put it
      // at negative coordinates inside an overflow-auto container, which
      // clamps scrollLeft/scrollTop to 0 — so the node became permanently
      // invisible and unreachable, while staying in the swarm, keeping its
      // connections, and getting written into the saved YAML.
      onMoveBot(
        instanceId,
        Math.max(0, originX + (ev.clientX - startX)),
        Math.max(0, originY + (ev.clientY - startY)),
      );
    };
    const onUp = () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  };

  const canvasPoint = (e: { clientX: number; clientY: number }) => {
    const box = canvasRef.current?.getBoundingClientRect();
    if (!box) return { x: 0, y: 0 };
    return {
      x: e.clientX - box.left + (canvasRef.current?.scrollLeft ?? 0),
      y: e.clientY - box.top + (canvasRef.current?.scrollTop ?? 0),
    };
  };

  // Holds the active connection drag's teardown so Escape can actually
  // remove its window listeners, not just hide the preview line — otherwise
  // a stray mouseup anywhere later would still silently try to complete it.
  const cancelConnectionRef = useRef<(() => void) | null>(null);

  const startConnection = (instanceId: string, port: string, e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setConnecting({ instanceId, port });
    setCursor(canvasPoint(e));
    const onMove = (ev: MouseEvent) => setCursor(canvasPoint(ev));
    const cleanup = () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      cancelConnectionRef.current = null;
    };
    const onUp = (ev: MouseEvent) => {
      cleanup();
      const target = document.elementFromPoint(ev.clientX, ev.clientY);
      const portEl = target?.closest("[data-port]") as HTMLElement | null;
      const targetKey = portEl?.dataset.port;
      setConnecting(null);
      setCursor(null);
      if (!targetKey) return;
      const [targetInstance, dir, ...rest] = targetKey.split(":");
      const targetPort = rest.join(":");
      if (dir !== "in" || targetInstance === instanceId) return;
      onAddSnap({ from: `${instanceId}.${port}`, to: `${targetInstance}.${targetPort}` });
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    cancelConnectionRef.current = cleanup;
  };

  const checkFor = (from: string, to: string) => snapChecks.find((c) => c.From === from && c.To === to);

  return (
    <div
      ref={canvasRef}
      className="relative h-full min-h-[420px] w-full overflow-auto bg-[radial-gradient(circle,theme(colors.edge)_1px,transparent_1px)] bg-[length:20px_20px]"
    >
      {/* z-20 puts the connector layer above the nodes (z-10). The layer
          itself is pointer-events-none so it never steals a click meant for
          a node; only the remove handles inside it opt back in. Without
          this, a node dragged over a connection's midpoint covered its
          remove handle completely and the connection became impossible to
          delete — there is no other affordance for removing one. */}
      <svg className="pointer-events-none absolute left-0 top-0 z-20 h-[2000px] w-[2000px] overflow-visible">
        {snaps.map((s, i) => {
          const fromEp = endpointPort(s.from);
          const toEp = endpointPort(s.to);
          const from = positions[portKey(fromEp.instanceId, "out", fromEp.port)];
          const to = positions[portKey(toEp.instanceId, "in", toEp.port)];
          if (!from || !to) return null;
          const check = checkFor(s.from, s.to);
          // Themed via CSS vars rather than hex so the canvas follows
          // light/dark like everything else — see src/index.css.
          const stroke = check
            ? check.OK
              ? "rgb(var(--c-ok))"
              : "rgb(var(--c-danger))"
            : "rgb(var(--c-tron))";
          const midX = (from.x + to.x) / 2;
          return (
            <g key={s.from + "->" + s.to} className="pointer-events-auto">
              <path
                d={`M ${from.x} ${from.y} C ${midX} ${from.y}, ${midX} ${to.y}, ${to.x} ${to.y}`}
                fill="none"
                stroke={stroke}
                strokeWidth={2}
                opacity={0.85}
              />
              <circle
                cx={(from.x + to.x) / 2}
                cy={(from.y + to.y) / 2}
                r={7}
                fill="rgb(var(--c-panel))"
                stroke={stroke}
                strokeWidth={1.5}
                className="cursor-pointer"
                onClick={() => onRemoveSnap(i)}
              >
                <title>Remove connection</title>
              </circle>
            </g>
          );
        })}
        {connecting && cursor && positions[portKey(connecting.instanceId, "out", connecting.port)] && (
          <line
            x1={positions[portKey(connecting.instanceId, "out", connecting.port)].x}
            y1={positions[portKey(connecting.instanceId, "out", connecting.port)].y}
            x2={cursor.x}
            y2={cursor.y}
            stroke="rgb(var(--c-tron))"
            strokeWidth={2}
            strokeDasharray="4 3"
          />
        )}
      </svg>

      {bots.map((bot) => {
        const def = botDefs[bot.botId];
        const rows = Math.max(def?.inputs.length ?? 0, def?.outputs.length ?? 0, 1);
        return (
          <div
            key={bot.instanceId}
            style={{ left: bot.x, top: bot.y, width: NODE_WIDTH, zIndex: 10 }}
            className={`absolute select-none rounded-lg border bg-panel shadow-glow-sm ${
              selectedInstanceId === bot.instanceId ? "border-tron" : "border-edge-strong"
            }`}
            onMouseDown={(e) => startDragNode(bot.instanceId, e)}
          >
            <div
              className="flex cursor-grab items-center justify-between gap-2 rounded-t-lg border-b border-edge px-3 py-2 active:cursor-grabbing"
              style={{ height: HEADER_HEIGHT }}
            >
              <div className="min-w-0">
                <div className="truncate font-display text-xs font-semibold text-ink">
                  {def?.name ?? bot.botId}
                </div>
                <div className="truncate text-[10px] text-muted">{bot.instanceId}</div>
              </div>
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  onRemoveBot(bot.instanceId);
                }}
                className="shrink-0 text-muted hover:text-danger"
                title="Remove from swarm"
              >
                ✕
              </button>
            </div>

            <div className="relative py-1.5" style={{ height: rows * ROW_HEIGHT }}>
              {def?.inputs.map((p, i) => (
                <div
                  key={p.name}
                  className="absolute left-0 flex -translate-x-1/2 items-center gap-1.5"
                  style={{ top: i * ROW_HEIGHT + ROW_HEIGHT / 2 - 7 }}
                >
                  <span
                    data-port={portKey(bot.instanceId, "in", p.name)}
                    ref={registerPort(portKey(bot.instanceId, "in", p.name))}
                    title={`${p.name}: ${p.type}`}
                    className="h-3.5 w-3.5 cursor-crosshair rounded-full border-2 border-muted bg-void hover:border-tron"
                  />
                  <span className="max-w-[110px] truncate pl-1 text-[10px] text-muted">{p.name}</span>
                </div>
              ))}
              {def?.outputs.map((p, i) => (
                <div
                  key={p.name}
                  className="absolute right-0 flex translate-x-1/2 items-center justify-end gap-1.5"
                  style={{ top: i * ROW_HEIGHT + ROW_HEIGHT / 2 - 7 }}
                >
                  <span className="max-w-[110px] truncate pr-1 text-right text-[10px] text-muted">
                    {p.name}
                  </span>
                  <span
                    data-port={portKey(bot.instanceId, "out", p.name)}
                    ref={registerPort(portKey(bot.instanceId, "out", p.name))}
                    onMouseDown={(e) => startConnection(bot.instanceId, p.name, e)}
                    title={`${p.name}: ${p.type}`}
                    className="h-3.5 w-3.5 cursor-crosshair rounded-full border-2 border-tron bg-void shadow-[0_0_6px_theme(colors.tron)] hover:bg-tron/30"
                  />
                </div>
              ))}
            </div>
          </div>
        );
      })}

      {bots.length === 0 && (
        <div className="flex h-full items-center justify-center text-sm text-muted">
          Click a bot on the left to add it here, then drag from an output dot to an input dot to connect them.
        </div>
      )}
    </div>
  );
}
