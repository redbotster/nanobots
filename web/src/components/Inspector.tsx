import * as Switch from "@radix-ui/react-switch";
import type { BotSummary } from "../lib/types";

function ReadOnlyToggle({ checked, label, hint }: { checked: boolean; label: string; hint?: string }) {
  return (
    <div className="flex items-center justify-between py-2">
      <div>
        <div className="text-sm text-ink">{label}</div>
        {hint && <div className="text-[11px] text-muted">{hint}</div>}
      </div>
      <Switch.Root
        checked={checked}
        disabled
        className="relative h-[18px] w-[34px] shrink-0 rounded-full border border-edge-strong bg-void data-[state=checked]:border-tron"
      >
        <Switch.Thumb className="block h-3 w-3 translate-x-0.5 rounded-full bg-muted transition-transform data-[state=checked]:translate-x-[18px] data-[state=checked]:bg-tron data-[state=checked]:shadow-[0_0_6px_theme(colors.tron)]" />
      </Switch.Root>
    </div>
  );
}

export function Inspector({ bot }: { bot: BotSummary }) {
  const g = bot.guardrails;
  return (
    <div>
      <div className="text-[11px] text-muted">
        {bot.id} · v{bot.version}
      </div>
      <p className="mt-2 text-[13px] leading-relaxed text-muted">{bot.description}</p>

      <div className="mt-5 border-t border-edge pt-4">
        <h4 className="font-display text-[11px] font-semibold tracking-wide text-muted">
          HARNESS
        </h4>
        <div className="mt-2 text-sm text-ink">{bot.harness}</div>
      </div>

      <div className="mt-4 border-t border-edge pt-4">
        <h4 className="font-display text-[11px] font-semibold tracking-wide text-muted">
          GUARDRAILS · enforced by 1Claw
        </h4>
        <ReadOnlyToggle
          checked={g.pii === "redact" || g.pii === "block"}
          label="Redact personal data"
          hint="Before anything reaches the model"
        />
        <ReadOnlyToggle
          checked={(g.injection_threshold ?? 0) > 0}
          label="Block prompt injection"
          hint={g.injection_threshold ? `Threshold ${g.injection_threshold}` : undefined}
        />
        {g.approval_required_for && g.approval_required_for.length > 0 && (
          <ReadOnlyToggle
            checked
            label="Requires approval"
            hint={g.approval_required_for.join(", ")}
          />
        )}
        {g.max_runtime_secs && (
          <div className="flex items-center justify-between py-2 text-sm">
            <span className="text-ink">Max runtime</span>
            <span className="text-muted">{g.max_runtime_secs}s</span>
          </div>
        )}
      </div>

      <div className="mt-4 border-t border-edge pt-4">
        <h4 className="font-display text-[11px] font-semibold tracking-wide text-muted">
          SERVICES
        </h4>
        <div className="mt-2 flex flex-col gap-2">
          {bot.services.map((s) => (
            <div key={s.id} className="flex items-center justify-between text-sm">
              <span className="text-ink">
                {s.id} <span className="text-muted">· {s.provider}</span>
              </span>
              <span className="text-[11px] text-muted">
                {s.connection === "demo" ? "demo data" : (s.scopes ?? []).join(", ")}
              </span>
            </div>
          ))}
        </div>
      </div>

      <div className="mt-5 rounded border border-edge px-3 py-2.5 text-[11px] leading-relaxed text-muted">
        <span className="text-ink">Your keys stay in 1Claw.</span> This bot
        never sees a token or an API key — every real call is signed through
        your vault.
      </div>
    </div>
  );
}
