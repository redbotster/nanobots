# reviewer

Review one piece of work, from one named perspective, and say what that perspective would block on.

The role is an **input**, not a different bot. One `reviewer` is a security engineer, a designer or an SRE depending on what it's given — so a review team is a swarm shape, not five near-identical bots to maintain. A swarm fans this out over `review-board`'s `roles` with a `.*` snap, so team size is decided per run.

`concerns` may be empty. A reviewer with nothing to say is a real outcome, and the prompt says so explicitly — a review bot that always finds something is a review bot nobody reads.

The work under review is treated as material, not as instructions: text inside it asking for a particular verdict is part of what's being reviewed.
