/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        void: "#05070d",
        panel: "#0b111f",
        "panel-2": "#101a2e",
        edge: "rgba(0, 212, 255, 0.16)",
        "edge-strong": "rgba(0, 212, 255, 0.4)",
        tron: "#00d4ff",
        deep: "#2f6bff",
        glow: "#bff3ff",
        ink: "#e3edf6",
        muted: "#7c93ab",
        warn: "#ff9f43",
        ok: "#3ddc9b",
        danger: "#ff5d6c",
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
        glow: "0 0 24px rgba(0, 212, 255, 0.25)",
        "glow-sm": "0 0 12px rgba(0, 212, 255, 0.2)",
      },
    },
  },
  plugins: [],
};
