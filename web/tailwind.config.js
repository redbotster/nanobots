/** @type {import('tailwindcss').Config} */

// Every color is a CSS variable so the whole app can swap themes without a
// single component knowing a theme exists — see src/index.css for the light
// and dark values. The channel form (`R G B`, not `rgb(...)`) is what lets
// Tailwind's opacity modifiers keep working: `bg-danger/10` compiles to
// `rgb(var(--c-danger) / 0.1)`.
const channel = (name) => `rgb(var(--c-${name}) / <alpha-value>)`;

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  darkMode: ["selector", '[data-theme="dark"]'],
  theme: {
    extend: {
      colors: {
        void: channel("void"),
        panel: channel("panel"),
        "panel-2": channel("panel-2"),
        // The two edge tokens carry their own alpha (they're hairlines, not
        // surfaces) and are never used with an opacity modifier, so they
        // stay whole colors rather than channels.
        edge: "var(--c-edge)",
        "edge-strong": "var(--c-edge-strong)",
        tron: channel("tron"),
        deep: channel("deep"),
        glow: channel("glow"),
        ink: channel("ink"),
        muted: channel("muted"),
        warn: channel("warn"),
        ok: channel("ok"),
        danger: channel("danger"),
      },
      fontFamily: {
        // Was Chakra Petch, a sci-fi display face that matched the old
        // neon-grid look and nothing else about the app. Plus Jakarta Sans
        // keeps headings visually distinct from body text (still its own
        // font, still bolder) without reading as a technical readout.
        display: ["'Plus Jakarta Sans'", "system-ui", "sans-serif"],
        body: ["-apple-system", "BlinkMacSystemFont", "'Segoe UI'", "system-ui", "sans-serif"],
      },
      boxShadow: {
        // Was a colored neon glow (0 0 24px, spreading evenly in every
        // direction — the classic "this button is plugged in" effect). A
        // real drop shadow, offset and softly blurred, reads as a calm
        // elevated surface instead of a light source.
        glow: "0 6px 20px -4px var(--c-glow-shadow)",
        "glow-sm": "0 2px 10px -2px var(--c-glow-shadow-sm)",
      },
      borderRadius: {
        // The default scale (lg = 0.5rem) is what read as technical next
        // to muse.ai's reference — cards and inputs there are closer to a
        // pill than a rounded rectangle. Bumped once, centrally: every
        // existing rounded-lg/xl in the app (34 uses) gets softer for free.
        lg: "0.875rem",
        xl: "1.25rem",
      },
    },
  },
  plugins: [],
};
