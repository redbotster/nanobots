import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { BotSummary, SnapCheck } from "../../lib/types";

export interface PlacedBot {
  instanceId: string;
  botId: string;
  x: number;
  y: number;
  /** "continue" keeps this bot's failure from ending the run — see
   * docs/error-policy.md. Undefined means the default, "stop". */
  onError?: string;
}

export interface CanvasSnap {
  from: string; // "<instanceId>.<port>"
  to: string;
  /** How a fanned-out list collapses into one value on the way through —
   * lines | json | count | flatten | first. See docs/fan-out.md. */
  join?: string;
}

const NODE_WIDTH = 208;
const ROW_HEIGHT = 24;
const HEADER_HEIGHT = 40;
const NODE_PADDING_Y = 12; // the port block's py-1.5, top and bottom

const MIN_ZOOM = 0.2;
const MAX_ZOOM = 1.5;
/** Breathing room around the graph when fitting, in content pixels — a graph
 * pinned to the exact edges of the viewport reads as cut off. */
const FIT_MARGIN = 48;
/** How far past the graph you can scroll, so there is somewhere to drag a
 * node *to*. Without it the canvas ends at the rightmost node and laying out
 * a new branch means dragging into a wall. */
const SCROLL_MARGIN = 600;

function nodeHeight(rows: number) {
  return HEADER_HEIGHT + NODE_PADDING_Y + rows * ROW_HEIGHT;
}

function clampZoom(z: number) {
  return Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, z));
}

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
  // The discrete, two-activation form of a connection drag: pick an output,
  // then pick an input. The canvas was previously mouse-only — ports were
  // bare spans with onMouseDown, so a keyboard user could add bots but
  // never wire them, which is the builder's entire purpose.
  const [pending, setPending] = useState<{ instanceId: string; port: string } | null>(null);
  const [cursor, setCursor] = useState<{ x: number; y: number } | null>(null);
  const [zoom, setZoom] = useState(1);
  // Read inside window-level drag handlers, which close over the zoom that
  // was current when the drag began. A ref keeps them honest if the zoom
  // changes mid-drag (ctrl+wheel while dragging is a real thing people do).
  const zoomRef = useRef(1);
  zoomRef.current = zoom;
  // Fit once, when a swarm's nodes and their port definitions have both
  // arrived. Not on every change: re-fitting under someone who has just
  // panned somewhere deliberately is the canvas fighting them.
  const fittedRef = useRef(false);
  // True when Fit wanted to zoom out further than MIN_ZOOM allows, so the
  // graph still doesn't all fit. Without this, pressing Fit on a very spread
  // out swarm looks like a button that does nothing.
  const [fitClamped, setFitClamped] = useState(false);

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
    // getBoundingClientRect reports the *scaled* on-screen box, while the
    // SVG that draws the connectors lives inside the scaled layer and thinks
    // in content pixels. Dividing by the zoom is what keeps a connector
    // attached to its port at any zoom level.
    const z = zoomRef.current;
    for (const [key, el] of portEls.current) {
      const box = el.getBoundingClientRect();
      next[key] = {
        x: (box.left + box.width / 2 - canvasBox.left + canvas.scrollLeft) / z,
        y: (box.top + box.height / 2 - canvasBox.top + canvas.scrollTop) / z,
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
      if (e.key === "Escape") {
        if (connecting) {
          cancelConnectionRef.current?.();
          setConnecting(null);
          setCursor(null);
        }
        setPending(null);
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
      // Divided by the zoom, so the node tracks the pointer rather than
      // running away from it at 50% or lagging behind at 150%.
      const z = zoomRef.current;
      // Clamped at the origin: dragging a node up and left past 0,0 put it
      // at negative coordinates inside an overflow-auto container, which
      // clamps scrollLeft/scrollTop to 0 — so the node became permanently
      // invisible and unreachable, while staying in the swarm, keeping its
      // connections, and getting written into the saved YAML.
      onMoveBot(
        instanceId,
        Math.max(0, originX + (ev.clientX - startX) / z),
        Math.max(0, originY + (ev.clientY - startY) / z),
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
    const z = zoomRef.current;
    return {
      x: (e.clientX - box.left + (canvasRef.current?.scrollLeft ?? 0)) / z,
      y: (e.clientY - box.top + (canvasRef.current?.scrollTop ?? 0)) / z,
    };
  };

  // The graph's own bounding box, in content pixels. Everything about
  // fitting and scroll extent is derived from this rather than from a
  // hardcoded 2000x2000, which was both too small for a wide graph and too
  // large for a two-node one.
  // useCallback so fitToView can depend on it by name. It reads only bots
  // and botDefs, which were fitToView's dependency list anyway — stating
  // that here instead means the linter can see the connection rather than
  // being told to trust it.
  const bounds = useCallback(() => {
    if (bots.length === 0) return null;
    let minX = Infinity,
      minY = Infinity,
      maxX = -Infinity,
      maxY = -Infinity;
    for (const b of bots) {
      const def = botDefs[b.botId];
      const rows = Math.max(def?.inputs.length ?? 0, def?.outputs.length ?? 0, 1);
      minX = Math.min(minX, b.x);
      minY = Math.min(minY, b.y);
      maxX = Math.max(maxX, b.x + NODE_WIDTH);
      maxY = Math.max(maxY, b.y + nodeHeight(rows));
    }
    return { minX, minY, maxX, maxY };
  }, [bots, botDefs]);

  const box = bounds();
  // The scrollable content area: past the graph by SCROLL_MARGIN so there is
  // room to drag a node somewhere new.
  const contentW = Math.max(1200, (box?.maxX ?? 0) + SCROLL_MARGIN);
  const contentH = Math.max(800, (box?.maxY ?? 0) + SCROLL_MARGIN);

  /** Scale and scroll so the whole graph is on screen. Never zooms past 1:1
   * — blowing a two-node swarm up to 150% to "fill" the canvas is not what
   * anyone means by Fit. */
  const fitToView = useCallback(() => {
    const canvas = canvasRef.current;
    const b = bounds();
    if (!canvas || !b) return;
    const availW = canvas.clientWidth - FIT_MARGIN * 2;
    const availH = canvas.clientHeight - FIT_MARGIN * 2;
    if (availW <= 0 || availH <= 0) return;
    const wanted = Math.min(1, availW / (b.maxX - b.minX), availH / (b.maxY - b.minY));
    const z = clampZoom(wanted);
    setFitClamped(wanted < MIN_ZOOM);
    zoomRef.current = z;
    setZoom(z);
    // After the transform lands, put the graph's top-left corner just inside
    // the margin. requestAnimationFrame because scrollTo before the layer
    // has re-rendered at the new scale clamps against the old extent.
    requestAnimationFrame(() => {
      canvas.scrollTo({
        left: Math.max(0, b.minX * z - FIT_MARGIN),
        top: Math.max(0, b.minY * z - FIT_MARGIN),
      });
    });
  }, [bounds]);

  // Fit once the nodes and their port definitions have both arrived. Opening
  // a five-bot swarm used to land you at 0,0 at 100% with the last two bots
  // off the right edge and nothing saying the canvas scrolled.
  useEffect(() => {
    if (fittedRef.current || bots.length === 0) return;
    if (!bots.every((b) => botDefs[b.botId])) return;
    fittedRef.current = true;
    fitToView();
  }, [bots, botDefs, fitToView]);

  /** Zoom about a point, keeping whatever is under it under it.
   * Zooming about the top-left instead makes the thing you were looking at
   * slide off screen, which is why every canvas app anchors on the cursor. */
  const zoomAbout = (nextZoom: number, clientX?: number, clientY?: number) => {
    const canvas = canvasRef.current;
    const z1 = clampZoom(nextZoom);
    if (!canvas) {
      zoomRef.current = z1;
      setZoom(z1);
      return;
    }
    const rect = canvas.getBoundingClientRect();
    const px = clientX ?? rect.left + canvas.clientWidth / 2;
    const py = clientY ?? rect.top + canvas.clientHeight / 2;
    const offX = px - rect.left;
    const offY = py - rect.top;
    const contentX = (offX + canvas.scrollLeft) / zoomRef.current;
    const contentY = (offY + canvas.scrollTop) / zoomRef.current;
    zoomRef.current = z1;
    setZoom(z1);
    requestAnimationFrame(() => {
      canvas.scrollLeft = Math.max(0, contentX * z1 - offX);
      canvas.scrollTop = Math.max(0, contentY * z1 - offY);
    });
  };

  // ctrl/cmd + wheel is the gesture people already have in their hands, and
  // a trackpad pinch arrives as exactly this. Registered manually because
  // React's onWheel is passive, and a passive listener cannot preventDefault
  // — without which the browser zooms the whole page instead.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return;
      e.preventDefault();
      zoomAbout(zoomRef.current * (e.deltaY < 0 ? 1.1 : 1 / 1.1), e.clientX, e.clientY);
    };
    canvas.addEventListener("wheel", onWheel, { passive: false });
    return () => canvas.removeEventListener("wheel", onWheel);
  }, []);

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

  const checkFor = (from: string, to: string) =>
    snapChecks.find((c) => c.From === from && c.To === to);

  return (
    <div
      ref={canvasRef}
      className="relative h-full min-h-[420px] w-full overflow-auto bg-[radial-gradient(circle,theme(colors.edge)_1px,transparent_1px)] bg-[length:20px_20px]"
      style={{ backgroundSize: `${20 * zoom}px ${20 * zoom}px` }}
    >
      <ZoomControls
        zoom={zoom}
        onZoom={(z) => {
          setFitClamped(false);
          zoomAbout(z);
        }}
        onFit={fitToView}
        canFit={bots.length > 0}
        clamped={fitClamped}
      />
      {/* Two nested layers, and both are load-bearing. `transform: scale`
          does not change layout size, so the scroll container would think
          the content is always its unscaled extent — zooming out would leave
          a huge empty scroll area, zooming in would cut the graph off. The
          sizer carries the *scaled* extent as real layout; the layer inside
          it carries the transform and keeps thinking in content pixels, so
          every coordinate below (and the SVG) is unchanged. */}
      <div style={{ width: contentW * zoom, height: contentH * zoom }}>
        <div
          style={{
            width: contentW,
            height: contentH,
            transform: `scale(${zoom})`,
            transformOrigin: "0 0",
          }}
        >
          {/* z-20 puts the connector layer above the nodes (z-10). The layer
          itself is pointer-events-none so it never steals a click meant for
          a node; only the remove handles inside it opt back in. Without
          this, a node dragged over a connection's midpoint covered its
          remove handle completely and the connection became impossible to
          delete — there is no other affordance for removing one. */}
          <svg
            className="pointer-events-none absolute left-0 top-0 z-20 overflow-visible"
            width={contentW}
            height={contentH}
          >
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
                    className="cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-tron"
                    role="button"
                    tabIndex={0}
                    aria-label={`Remove the connection from ${s.from} to ${s.to}`}
                    onClick={() => onRemoveSnap(i)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        onRemoveSnap(i);
                      }
                    }}
                  >
                    <title>Remove connection</title>
                  </circle>
                </g>
              );
            })}
            {connecting &&
              cursor &&
              positions[portKey(connecting.instanceId, "out", connecting.port)] && (
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
                tabIndex={0}
                role="group"
                aria-label={`${bot.instanceId} (${bot.botId})${selectedInstanceId === bot.instanceId ? ", selected" : ""}`}
                onFocus={() => onSelectBot(bot.instanceId)}
                onKeyDown={(e) => {
                  // Arrow keys move a node without a mouse. Shift for a coarse
                  // step, since nudging 240px one press at a time is not a
                  // usable way to lay out a graph.
                  const step = e.shiftKey ? 40 : 8;
                  const deltas: Record<string, [number, number]> = {
                    ArrowLeft: [-step, 0],
                    ArrowRight: [step, 0],
                    ArrowUp: [0, -step],
                    ArrowDown: [0, step],
                  };
                  const d = deltas[e.key];
                  if (!d) return;
                  e.preventDefault();
                  onMoveBot(bot.instanceId, Math.max(0, bot.x + d[0]), Math.max(0, bot.y + d[1]));
                }}
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
                      <button
                        type="button"
                        data-port={portKey(bot.instanceId, "in", p.name)}
                        ref={registerPort(portKey(bot.instanceId, "in", p.name))}
                        onClick={(e) => {
                          e.stopPropagation();
                          if (!pending) return;
                          onAddSnap({
                            from: `${pending.instanceId}.${pending.port}`,
                            to: `${bot.instanceId}.${p.name}`,
                          });
                          setPending(null);
                        }}
                        disabled={!pending}
                        aria-label={
                          pending
                            ? `Connect ${pending.instanceId}.${pending.port} to input ${p.name} of ${bot.instanceId}`
                            : `Input ${p.name} of ${bot.instanceId}, type ${p.type}. Activate an output port first.`
                        }
                        title={`${p.name}: ${p.type}`}
                        className={`h-3.5 w-3.5 rounded-full border-2 bg-void focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-tron ${
                          pending
                            ? "cursor-crosshair border-tron hover:bg-tron/30"
                            : "cursor-default border-muted"
                        }`}
                      />
                      <span className="max-w-[110px] truncate pl-1 text-[10px] text-muted">
                        {p.name}
                      </span>
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
                      <button
                        type="button"
                        data-port={portKey(bot.instanceId, "out", p.name)}
                        ref={registerPort(portKey(bot.instanceId, "out", p.name))}
                        onMouseDown={(e) => startConnection(bot.instanceId, p.name, e)}
                        onClick={(e) => {
                          // Keyboard and plain-click path: arm this output, then
                          // activate an input port to complete the connection.
                          // The mouse drag above is the same operation done
                          // continuously; this is the discrete version.
                          e.stopPropagation();
                          setPending({ instanceId: bot.instanceId, port: p.name });
                        }}
                        aria-label={`Output ${p.name} of ${bot.instanceId}, type ${p.type}. Activate, then choose an input to connect it to.`}
                        title={`${p.name}: ${p.type}`}
                        className={`h-3.5 w-3.5 cursor-crosshair rounded-full border-2 border-tron bg-void shadow-[0_0_6px_theme(colors.tron)] hover:bg-tron/30 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-tron ${
                          pending?.instanceId === bot.instanceId && pending?.port === p.name
                            ? "bg-tron"
                            : ""
                        }`}
                      />
                    </div>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Outside the scaled layer, so the empty-state sentence stays
          readable at whatever zoom the last swarm left behind. */}
      {bots.length === 0 && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-sm text-muted">
          Click a bot on the left to add it here, then drag from an output dot to an input dot to
          connect them.
        </div>
      )}
    </div>
  );
}

/** Zoom out / level / zoom in / fit.
 *
 * Sticky rather than scrolling away with the canvas: the moment you need
 * these is when you are lost somewhere in a big graph, which is exactly when
 * a control parked at the content's origin is unreachable. */
function ZoomControls({
  zoom,
  onZoom,
  onFit,
  canFit,
  clamped,
}: {
  zoom: number;
  onZoom: (z: number) => void;
  onFit: () => void;
  canFit: boolean;
  /** Fit ran but the swarm is spread wider than the smallest readable zoom.
   * Saying so is the difference between "this button is broken" and "your
   * graph is bigger than the screen". */
  clamped: boolean;
}) {
  const btn =
    "flex h-7 w-7 items-center justify-center rounded border border-edge bg-panel/95 text-muted transition-colors hover:border-tron hover:text-ink disabled:opacity-40";
  return (
    <div className="sticky left-3 top-3 z-30 flex w-fit items-center gap-1 backdrop-blur">
      <button
        onClick={() => onZoom(zoom / 1.25)}
        disabled={zoom <= MIN_ZOOM + 0.001}
        className={btn}
        title="Zoom out"
        aria-label="Zoom out"
      >
        −
      </button>
      <button
        onClick={() => onZoom(1)}
        className={`h-7 rounded border bg-panel/95 px-2 font-mono text-[11px] transition-colors hover:border-tron hover:text-ink ${
          clamped ? "border-warn/50 text-warn" : "border-edge text-muted"
        }`}
        title={
          clamped
            ? "This is as far out as it goes and the swarm is still wider than the canvas — scroll to see the rest, or drag its nodes closer together."
            : "Back to 100%"
        }
        aria-label={`Zoom is ${Math.round(zoom * 100)} percent. Reset to 100 percent.`}
      >
        {Math.round(zoom * 100)}%
      </button>
      <button
        onClick={() => onZoom(zoom * 1.25)}
        disabled={zoom >= MAX_ZOOM - 0.001}
        className={btn}
        title="Zoom in"
        aria-label="Zoom in"
      >
        +
      </button>
      <button
        onClick={onFit}
        disabled={!canFit}
        className="h-7 rounded border border-edge bg-panel/95 px-2 text-[11px] text-muted transition-colors hover:border-tron hover:text-ink disabled:opacity-40"
        title="Fit the whole swarm on screen"
      >
        Fit
      </button>
    </div>
  );
}
