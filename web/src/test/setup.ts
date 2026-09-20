import "@testing-library/jest-dom/vitest";

// jsdom doesn't implement scrollIntoView at all (throws "not a function"),
// unlike a real browser. RunLog and LabPage both auto-scroll their log to
// the newest entry; without this, mounting either in a test with any log
// content throws from inside a useEffect.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}
