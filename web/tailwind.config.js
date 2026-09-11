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
        display: ["'Chakra Petch'", "system-ui", "sans-serif"],
        body: [
          "-apple-system",
          "BlinkMacSystemFont",
          "'Segoe UI'",
          "system-ui",
          "sans-serif",
        ],
      },
      boxShadow: {
        glow: "0 0 24px var(--c-glow-shadow)",
        "glow-sm": "0 0 12px var(--c-glow-shadow-sm)",
      },
    },
  },
  plugins: [],
};
