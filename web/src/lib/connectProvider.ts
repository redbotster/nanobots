import { api } from "./api";
import type { ConnectableService } from "./types";

const OAUTH_STARTERS: Partial<Record<ConnectableService, () => Promise<unknown>>> = {
  google: api.connectGoogleStart,
  x: api.connectXStart,
  linkedin: api.connectLinkedInStart,
};

/** True for a provider that connects via a real OAuth round trip (opens
 * the browser) rather than a pasted static token — callers use this to
 * decide whether "connect" is a single click or needs an inline field for
 * the token first (see BotCard's ServiceToggle for the token-path UI). */
export function isOAuthProvider(service: ConnectableService): boolean {
  return service in OAUTH_STARTERS;
}

/** Starts an OAuth provider's real connect flow (opens the browser, blocks
 * until the human finishes). Only valid for isOAuthProvider(service). */
export async function startOAuthConnect(service: ConnectableService): Promise<void> {
  const start = OAUTH_STARTERS[service];
  if (!start) throw new Error(`${service} isn't an OAuth provider`);
  await start();
}
