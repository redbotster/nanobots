# approve

Pause the swarm until a human approves on web or phone.

`bare` harness, one step. Unlike an inline `approve` step inside another bot (which halts that bot outright on a "no" — see `email-drive-file`'s `gate`), this brick's whole job is to *report* the decision as data: `approved:boolean` and `decided_by:string`. A rejection here is a normal result, not a failure — the swarm around this brick decides what happens next (skip a step, notify someone, try again later), not this bot.
