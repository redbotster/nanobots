# The visual builder

## What it can edit, and what it only preserves

The builder round-trips a *whole* swarm on every save — the request it
sends fully replaces the file's bots and snaps (`internal/api/swarmmerge.go`),
so a field it doesn't carry through its own load/save state is a field a
save silently deletes. That is not hypothetical: it once removed a swarm's
cron trigger and its guardrails, then separately dropped `on_error` and
`join` before either had an editor.

Per bot instance, today:

| Field | Editable in the Inspector | Preserved if already set |
|---|---|---|
| inputs | yes | yes |
| `on_error` | yes (a select) | yes |
| `join` (on a snap) | yes (a dropdown) | yes |
| `retry`, `execution` | no | yes |
| `retry_backoff`, `when`, `fallback`, `loop` | no | yes |
| `swarm` (nests another swarm as this bot) | no | yes — see below |

"Preserved" means exactly that: open a swarm that already sets `when:` on
a bot, change nothing about that bot, save, and it comes back out with the
same `when:`. There is no control to add one from scratch, and no way to
see its value without switching to the swarm's own YAML. Building or
editing one of these requires hand-editing the file — see
[anatomy.md](anatomy.md) for what the raw YAML looks like.

A nested-swarm bot (`swarm:` set instead of `use:`) is the one case worth
naming specifically: it has no catalog bot id at all, so the canvas has no
icon or name for it — it renders as a blank node. Saving it is still safe
(`use:` is never reconstructed for one; see `toDraftBot` in
`web/src/components/builder/hydrate.ts`), just not yet visually
represented. The composer never proposes one either, for the same reason
it can't invent a path to a swarm file that doesn't exist yet from a single
sentence (`internal/api/compose.go`'s prompt says so explicitly).

The composer, unlike the builder, *can* generate `retry`, `retry_backoff`,
`when`, `fallback` and `loop` in a swarm it drafts from a request — "retry
up to 3 times", "only notify if the amount is over $500", "keep fetching
the next page until there isn't one" all reach the model's prompt. What it
produces still has no visual editor once it lands in the builder for
review — the fields above are why.

## Zoom, and fitting the graph

Opening a five-bot swarm used to land you at 0,0 at 100% with the last two
bots off the right edge, and nothing saying the canvas scrolled. It now
fits on open: once the nodes and their port definitions have both arrived,
the canvas scales so the whole graph is on screen. Once only — re-fitting
under someone who has just panned somewhere deliberately is the canvas
fighting them.

Controls sit top-left and stay there while you scroll: **−**, the current
level (click to snap back to 100%), **+**, and **Fit**. `ctrl`/`cmd` +
wheel — which is what a trackpad pinch arrives as — zooms about the cursor,
so whatever you were looking at stays where it was.

Zoom runs from 20% to 150%. Fit never zooms *in* past 1:1, because blowing
a two-node swarm up to 150% to fill the canvas is not what anyone means by
Fit. If the graph is spread wider than 20% can show, Fit gets as close as
it can and the level readout turns amber rather than looking like a button
that did nothing.

### How it works, and why that shape

One transformed layer, not per-node scaling:

```
scroll container            overflow-auto
└── sizer                   width/height = content × zoom   (real layout)
    └── scaled layer        transform: scale(z), origin 0 0
        ├── <svg>           connectors, in content pixels
        └── nodes           left/top in content pixels
```

Both wrappers are load-bearing. `transform: scale` does not change layout
size, so with only the transform the scroll container would always think
the content is its unscaled extent — zooming out would leave a huge empty
scroll area, zooming in would cut the graph off. The sizer carries the
scaled extent as real layout; the layer inside keeps thinking in content
pixels, so node coordinates and the connector SVG are untouched by zoom.

Three places convert between screen and content pixels, and all three
divide by the zoom: measuring port centres (`measure`), the pointer during
a connection drag (`canvasPoint`), and the delta during a node drag. Miss
any one and it fails visibly — connectors detach from their ports, or a
dragged node runs away from the cursor.

Verified in a browser at 80% and 125%: every connector's path endpoints
land within 1.5px of their port centres, a 100px cursor drag moves a node
by exactly 100/zoom content pixels, and ctrl+wheel holds the anchor point
under the cursor to within half a pixel.
