// The five-minute first run, checked in a real browser.
//
// Every other test in this repo proves a piece. This one proves the claim
// the README opens with: clone it, start it, and a swarm runs to success
// with nothing configured — no 1Claw account, no model key, no OAuth app,
// no Docker for the bot itself.
//
// It is a separate npm project from web/ on purpose. Puppeteer downloads a
// Chromium, and every contributor running `npm ci` in web/ should not pay
// for that to typecheck a component.
//
//   cd scripts/e2e && npm install
//   npx tsx first-run.ts                      # against http://127.0.0.1:7474
//   npx tsx first-run.ts http://host:port     # against somewhere else
//
// Exits 0 when the run succeeded, 1 with a screenshot in ./artifacts when it
// did not. The screenshot is the point: a failure here is usually visual,
// and "the button was not there" is a sentence a log cannot write.

import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import puppeteer, { type Browser, type Page } from "puppeteer";

const BASE = process.argv[2] ?? "http://127.0.0.1:7474";
const ARTIFACTS = join(import.meta.dirname, "artifacts");

// supervisor-review needs nothing connected — it only reasons — so it is the
// one swarm whose success proves the unconfigured path rather than proving
// this machine happens to have Gmail wired up.
const SWARM = "supervisor-review";

// Generous, because this is measuring a first run on a cold machine, not
// measuring latency. A real hang still ends the script rather than the job's
// six-hour ceiling.
const RUN_TIMEOUT_MS = 180_000;

async function main() {
  await mkdir(ARTIFACTS, { recursive: true });
  const browser = await puppeteer.launch({
    headless: true,
    args: ["--no-sandbox", "--disable-dev-shm-usage"],
  });
  const page = await browser.newPage();
  await page.setViewport({ width: 1440, height: 900 });

  // Anything the page logs as an error is worth seeing in the job output —
  // a white screen from a bad bundle is otherwise indistinguishable from a
  // slow one.
  const consoleErrors: string[] = [];
  page.on("console", (m) => m.type() === "error" && consoleErrors.push(m.text()));
  page.on("pageerror", (e) => consoleErrors.push(`uncaught: ${e.message}`));

  try {
    await step(page, "the landing page loads", async () => {
      const res = await page.goto(BASE, { waitUntil: "domcontentloaded", timeout: 30_000 });
      if (!res || !res.ok()) throw new Error(`GET ${BASE} answered ${res?.status()}`);
      // Not networkidle: the runs list polls, so the network is never idle.
      await page.waitForFunction(
        () => document.body.innerText.includes("Open the app"),
        { timeout: 30_000 },
      );
    });

    // A fresh browser profile has never been here, so it gets the landing
    // page — the same thing a new user gets. lib/entered.ts remembers the
    // click, which is why this is one step rather than one per page load.
    await step(page, "it opens into the app", async () => {
      await clickByText(page, "button", "Open the app");
      await page.waitForSelector("h2", { timeout: 30_000 });
    });

    await step(page, `the gallery offers ${SWARM}`, async () => {
      await page.waitForFunction(
        (name: string) =>
          [...document.querySelectorAll("h2")].some((h) => h.textContent?.trim() === name),
        { timeout: 30_000 },
        SWARM,
      );
    });

    await step(page, "opening it shows the swarm", async () => {
      await clickByText(page, "h2", SWARM);
      await page.waitForFunction(
        () => document.body.innerText.includes("Run once"),
        { timeout: 15_000 },
      );
    });

    // Deliberately not "the word succeeded appears somewhere". The page
    // already carries the *previous* run's status — the gallery card shows
    // it and so does the swarm header — so that assertion passed in 559ms
    // against a run that takes twenty seconds, a green tick for work that
    // had not happened.
    //
    // Watching for "Running…" instead does not work either: with no model
    // configured every ai.generate step returns its fixture, so the whole
    // swarm can finish between two polls and that label never renders. The
    // run this click started is the only thing worth asserting on, so ask
    // which one it was.
    await step(page, "it runs to success on example data", async () => {
      const before = await newestRunID(page);
      await clickByText(page, "button", "Run once");

      const finished = await page.waitForFunction(
        async (previous: string | null) => {
          const runs = await fetch("/api/runs").then((r) => r.json());
          const newest = runs[0];
          if (!newest || newest.id === previous) return false;
          return newest.status === "running" || newest.status === "pending"
            ? false
            : { id: newest.id, status: newest.status, error: newest.error };
        },
        { timeout: RUN_TIMEOUT_MS, polling: 500 },
        before,
      );
      const run = (await finished.jsonValue()) as { id: string; status: string; error: string };
      if (run.status !== "succeeded") {
        throw new Error(`run ${run.id} finished ${run.status}: ${run.error || "no reason given"}`);
      }

      // And the page has to be showing that run, not the last one. The
      // results panel labels it with the id's first eight characters.
      await page.waitForFunction(
        (short: string) => document.body.innerText.includes(`run ${short}`),
        { timeout: 20_000, polling: 500 },
        run.id.slice(0, 8),
      );
    });

    // The whole point of the unconfigured path is that it says so. A run
    // that quietly looked real would be worse than a failure — so this
    // checks the specific banner, not merely that the word "demo" appears
    // somewhere, which the theme toggle's own label would satisfy.
    await step(page, "it says plainly that nothing is configured", async () => {
      const banner = await page.evaluate(() => {
        const text = document.body.innerText;
        return {
          noModel: text.includes("No model configured"),
          fixtures: /demo fixtures|example data|demo data/i.test(text),
        };
      });
      if (!banner.noModel || !banner.fixtures) {
        throw new Error(
          `the run succeeded without saying the output was not real ` +
            `(no-model banner: ${banner.noModel}, fixtures wording: ${banner.fixtures})`,
        );
      }
    });

    await page.screenshot({ path: join(ARTIFACTS, "first-run.png"), fullPage: true });
    if (consoleErrors.length > 0) {
      // Not a failure on its own — but unreported it never gets fixed.
      console.log(`\nconsole errors during the pass (${consoleErrors.length}):`);
      for (const e of consoleErrors.slice(0, 10)) console.log(`  ${e}`);
    }
    console.log(`\nfirst run OK against ${BASE}`);
  } catch (err) {
    await page.screenshot({ path: join(ARTIFACTS, "failure.png"), fullPage: true }).catch(() => {});
    await writeFile(
      join(ARTIFACTS, "failure.txt"),
      [
        `${err}`,
        "",
        `url: ${page.url()}`,
        `console errors: ${consoleErrors.length}`,
        ...consoleErrors,
        "",
        "page text:",
        await page.evaluate(() => document.body.innerText).catch(() => "(unreadable)"),
      ].join("\n"),
    ).catch(() => {});
    console.error(`\nFAILED: ${err}`);
    console.error(`artifacts in ${ARTIFACTS}`);
    await close(browser);
    process.exit(1);
  }
  await close(browser);
}

// The id of the most recent run, or null on a machine that has never run
// anything — which is what a genuinely first run looks like.
async function newestRunID(page: Page): Promise<string | null> {
  return page.evaluate(async () => {
    const runs = await fetch("/api/runs").then((r) => r.json());
    return runs[0]?.id ?? null;
  });
}

async function step(page: Page, what: string, fn: () => Promise<void>) {
  const started = Date.now();
  try {
    await fn();
  } catch (err) {
    throw new Error(`${what}: ${err}`);
  }
  console.log(`ok  ${what}  (${Date.now() - started}ms)`);
}

// Puppeteer has no text selector, and the app has no test ids — deliberately,
// since a selector that only exists for a test is a selector nobody notices
// breaking. This matches on what a person would read.
async function clickByText(page: Page, selector: string, text: string) {
  const handle = await page.evaluateHandle(
    (sel: string, want: string) =>
      [...document.querySelectorAll(sel)].find((el) => el.textContent?.trim().includes(want)),
    selector,
    text,
  );
  const element = handle.asElement();
  if (!element) throw new Error(`no ${selector} reading ${JSON.stringify(text)}`);
  await element.click();
}

async function close(browser: Browser) {
  await browser.close().catch(() => {});
}

await main();
