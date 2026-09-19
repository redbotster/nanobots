//go:build !race

package runner

// raceEnabled is true only in a `go test -race` build. The scale tests in
// scale_test.go assert real wall-clock numbers against SQLite, and the
// race detector's own instrumentation overhead — commonly 5-20x on
// memory-heavy code, not a regression in anything this repo wrote — pushed
// a 294ms reload past even a generous margin under `-race`, which CLAUDE.md
// requires running every time. The numbers those tests assert are the ones
// that matter for a real user's nanobotd; under `-race` they check
// direction (SQLite still faster than JSON) rather than an absolute bound.
const raceEnabled = false
