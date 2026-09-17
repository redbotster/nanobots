package runner

import "fmt"

// NothingToDoError is a bot that ended early on purpose because nothing had
// changed — a `stop.if` step matched (see internal/step).
//
// An error type for a thing that is not an error, because it travels the
// same path a failure does: runBot returns it, runOneBot passes it up, and
// runLevels is the one place that knows the difference. Modelling it as a
// success instead would mean a second return value on every layer in
// between, all of which would ignore it.
//
// What runLevels does with it is the point. Everything downstream is
// skipped, because their inputs genuinely never arrived; the run still
// succeeds, because looking and finding nothing is the correct outcome of a
// watch; and nothing is recorded as tolerated, because no failure was
// tolerated. A watch on an hourly cron should read as twenty-four quiet
// runs, not twenty-four warnings.
type NothingToDoError struct {
	Bot    string
	Reason string
}

func (e *NothingToDoError) Error() string {
	return fmt.Sprintf("bot %s had nothing to do: %s", e.Bot, e.Reason)
}
