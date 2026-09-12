import { api, subscribeFoundryEvents } from "./api";
import type { FoundryJob } from "./types";
import { useLiveJob } from "./useLiveJob";

/** One foundry job's live state — the same two channels as a swarm run,
 * because a foundry job embeds a runner.Run on the backend. See useLiveJob. */
export function useFoundryJob(jobId: string | null) {
  const { job, isTerminal } = useLiveJob<FoundryJob>(
    jobId,
    api.getFoundryJob,
    subscribeFoundryEvents,
  );
  return { job, isTerminal };
}
