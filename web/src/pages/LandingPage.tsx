import { Button } from "../components/Button";

const NAV_LINKS = [
  { label: "How it works", href: "#how-it-works" },
  { label: "Docs", href: "https://github.com/redbotster/nanobots/tree/main/docs" },
];

// TODO(nanobots#auth): "Login" just opens the local dashboard — this build
// has no session/identity system of its own, only the 1Claw Human API key
// nanobotd reads at startup (see docs/oneclaw-bridge.md). Once Nanobots is
// ever hosted for more than one person, this is where "Sign in with 1Claw"
// (OAuth + PKCE, blueprint §3.2) replaces this button's behavior — the UI
// shape stays the same.
export function LandingPage({ onLogin }: { onLogin: () => void }) {
  return (
    <div className="flex h-screen flex-col overflow-auto">
      <header className="flex items-center gap-6 border-b border-edge px-6 py-4 sm:px-10">
        <div className="flex items-center gap-2.5 font-display text-lg font-bold tracking-[0.14em] text-ink">
          <span className="inline-block h-5 w-2.5 bg-tron shadow-glow-sm" />
          nanobots
        </div>
        <nav className="ml-auto hidden items-center gap-6 sm:flex">
          {NAV_LINKS.map((link) => (
            <a
              key={link.label}
              href={link.href}
              className="text-sm text-muted hover:text-ink"
            >
              {link.label}
            </a>
          ))}
        </nav>
        <Button variant="ghost" onClick={onLogin}>
          Login
        </Button>
      </header>

      <main className="flex flex-1 flex-col items-center justify-center px-6 py-20 text-center sm:px-10">
        <div className="font-display text-xs font-semibold tracking-[0.2em] text-tron">
          LOCAL-FIRST · KEYS STAY IN 1CLAW
        </div>
        <h1 className="mt-4 max-w-2xl font-display text-3xl font-medium leading-tight text-ink sm:text-4xl">
          Legos for AI.
        </h1>
        <p className="mt-4 max-w-lg text-[15px] leading-relaxed text-muted">
          Snap micro-agents together into workflows that run on your own
          machine. Every credential, every approval, every guardrail lives in
          1Claw — nanobots never sees a token, and nothing leaves your inbox
          without you saying so.
        </p>
        <Button variant="primary" onClick={onLogin} className="mt-8 px-6 py-2.5 text-sm">
          Login
        </Button>

        <div
          id="how-it-works"
          className="mt-24 grid max-w-3xl grid-cols-1 gap-8 text-left sm:grid-cols-3"
        >
          {[
            {
              title: "Snap bots together",
              body: "Each bot does one job with typed ports. Output of one snaps into input of the next — the planner refuses anything that doesn't type-check.",
            },
            {
              title: "Watch it run, live",
              body: "Every step streams to a run log in real time, in a real sandboxed container — non-root, read-only filesystem, no exceptions.",
            },
            {
              title: "Approve before it acts",
              body: "Anything that sends, posts, pays, or deletes pauses for you first. You turn that off; it never ships off by default.",
            },
          ].map((f) => (
            <div key={f.title}>
              <h3 className="font-display text-sm font-semibold text-ink">
                {f.title}
              </h3>
              <p className="mt-1.5 text-[13px] leading-relaxed text-muted">
                {f.body}
              </p>
            </div>
          ))}
        </div>
      </main>

      <footer className="flex flex-col items-center gap-2 border-t border-edge px-6 py-6 text-xs text-muted sm:flex-row sm:justify-between sm:px-10">
        <div>nanobots · local-first · MIT licensed</div>
        <div className="flex gap-4">
          <a
            href="https://github.com/redbotster/nanobots"
            className="hover:text-ink"
          >
            GitHub
          </a>
          <a
            href="https://docs.1claw.co"
            className="hover:text-ink"
          >
            1Claw docs
          </a>
        </div>
      </footer>
    </div>
  );
}
