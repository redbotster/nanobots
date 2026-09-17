import { Button } from "../components/Button";

const NAV_LINKS = [
  { label: "How it works", href: "#how-it-works" },
  { label: "Docs", href: "https://github.com/redbotster/nanobots/tree/main/docs" },
];

// The button used to say "Login". There is no login: this build has no
// session or identity system of its own, only the 1Claw key nanobotd reads
// at startup (docs/oneclaw-bridge.md), so clicking it asked for nothing,
// protected nothing, and had to be clicked again on every reload. A browser
// pass of the first run tripped over it as a wall before the product.
//
// TODO(nanobots#auth): once Nanobots is ever hosted for more than one
// person, this is where "Sign in with 1Claw" (OAuth + PKCE, blueprint §3.2)
// goes — and then the label can honestly say Login again. The UI shape
// stays the same. lib/entered.ts remembers the choice meanwhile.
export function LandingPage({ onEnter }: { onEnter: () => void }) {
  return (
    <div className="flex h-screen flex-col overflow-auto">
      <header className="flex items-center gap-6 border-b border-edge px-6 py-4 sm:px-10">
        <div className="flex items-center gap-2.5 font-display text-lg font-bold tracking-[0.14em] text-ink">
          <span className="inline-block h-5 w-2.5 bg-tron shadow-glow-sm" />
          nanobots
        </div>
        <nav className="ml-auto hidden items-center gap-6 sm:flex">
          {NAV_LINKS.map((link) => (
            <a key={link.label} href={link.href} className="text-sm text-muted hover:text-ink">
              {link.label}
            </a>
          ))}
        </nav>
        <Button variant="ghost" onClick={onEnter}>
          Open the app
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
          Snap micro-agents together into workflows that run on your own machine. Every credential,
          every approval, every guardrail lives in 1Claw — nanobots never sees a token, and nothing
          leaves your inbox without you saying so.
        </p>
        <Button variant="primary" onClick={onEnter} className="mt-8 px-6 py-2.5 text-sm">
          Open the app
        </Button>

        <div className="mt-12 w-full max-w-xl rounded-lg border border-edge-strong bg-panel p-4 text-left shadow-glow-sm">
          <div className="flex items-center gap-2">
            <span className="text-base">✨</span>
            <span className="font-display text-xs font-semibold text-muted">
              Just say what you want
            </span>
          </div>
          <div className="mt-2 flex flex-col gap-2 sm:flex-row sm:items-center">
            <div className="flex-1 rounded border border-edge-strong bg-void px-3 py-2 text-[13px] text-ink">
              Help me automate a daily email recap and list it by priority
            </div>
            <span className="hidden shrink-0 text-tron sm:block">→</span>
            <span className="shrink-0 rounded border border-tron/40 bg-tron/5 px-2.5 py-2 text-center text-[12px] text-tron sm:text-left">
              a working swarm
            </span>
          </div>
          <p className="mt-2 text-[12px] leading-relaxed text-muted">
            No YAML, no drag-and-drop tutorial — the "head nanobot" snaps together a real, validated
            draft from the actual bot catalog and hands it to you to review.
          </p>
        </div>

        <div
          id="how-it-works"
          className="mt-16 grid max-w-4xl grid-cols-1 gap-8 text-left sm:grid-cols-4"
        >
          {[
            {
              title: "Describe it, or snap bots yourself",
              body: "Type what you want automated and let the head nanobot assemble it, or drag bots onto a canvas by hand — typed ports mean the planner refuses anything that doesn't type-check.",
            },
            {
              title: "Watch it run, live",
              body: "Every step streams to a run log in real time, in a real sandboxed container — non-root, read-only filesystem, no exceptions.",
            },
            {
              title: "Approve before it acts",
              body: "Anything that sends, posts, pays, or deletes pauses for you first. You turn that off; it never ships off by default.",
            },
            {
              title: "It runs itself from here",
              body: "Give it a schedule and walk away — a real cron scheduler fires it for you, no one clicking Run every morning.",
            },
          ].map((f) => (
            <div key={f.title}>
              <h3 className="font-display text-sm font-semibold text-ink">{f.title}</h3>
              <p className="mt-1.5 text-[13px] leading-relaxed text-muted">{f.body}</p>
            </div>
          ))}
        </div>
      </main>

      <footer className="flex flex-col items-center gap-2 border-t border-edge px-6 py-6 text-xs text-muted sm:flex-row sm:justify-between sm:px-10">
        <div>nanobots · local-first · source-available</div>
        <div className="flex gap-4">
          <a href="https://github.com/redbotster/nanobots" className="hover:text-ink">
            GitHub
          </a>
          <a href="https://docs.1claw.co" className="hover:text-ink">
            1Claw docs
          </a>
        </div>
      </footer>
    </div>
  );
}
