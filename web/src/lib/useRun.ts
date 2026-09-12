import { api, subscribeRunEvents } from "./api";
import type { Run } from "./types";
import { useLiveJob } from "./useLiveJob";

/** One swarm run's live state — status and approvals by poll, log tail by
 * SSE. See useLiveJob for how the two channels combine. */
export function useRun(runId: string | null) {
  const { job, isTerminal } = useLiveJob<Run>(runId, api.getRun, subscribeRunEvents);
  return { run: job, isTerminal };
}
