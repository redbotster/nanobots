#!/usr/bin/env node
// `npx nanobots` — download the right release binary once, then run it.
//
// A shim rather than a real npm package with the binary inside it: nanobots
// is a Go program, and shipping six platform tarballs through npm to avoid
// one download is more machinery than it saves. This keeps the npm side to
// one file whose only job is to pick the right asset.
//
// The binary is cached under the user's cache directory, so the second run
// is instant and works offline.

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const https = require("node:https");

const REPO = "redbotster/nanobots";

const GOOS = { darwin: "Darwin", linux: "Linux", win32: "Windows" }[process.platform];
const GOARCH = { x64: "x86_64", arm64: "arm64" }[process.arch];

if (!GOOS || !GOARCH) {
  console.error(
    `nanobots: no published build for ${process.platform}/${process.arch}.\n` +
      `Build from source instead: https://github.com/${REPO}#install`,
  );
  process.exit(1);
}

const cacheDir = path.join(
  process.env.XDG_CACHE_HOME || path.join(os.homedir(), ".cache"),
  "nanobots",
);
const exe = path.join(cacheDir, process.platform === "win32" ? "nanobots.exe" : "nanobots");

function get(url) {
  return new Promise((resolve, reject) => {
    https
      .get(url, { headers: { "User-Agent": "nanobots-npx" } }, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          res.resume();
          return resolve(get(res.headers.location));
        }
        if (res.statusCode !== 200) {
          res.resume();
          return reject(new Error(`${url} -> HTTP ${res.statusCode}`));
        }
        const chunks = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () => resolve(Buffer.concat(chunks)));
      })
      .on("error", reject);
  });
}

async function download() {
  process.stderr.write("nanobots: fetching the latest release...\n");
  const meta = JSON.parse(
    (await get(`https://api.github.com/repos/${REPO}/releases/latest`)).toString(),
  );
  const version = String(meta.tag_name || "").replace(/^v/, "");
  const ext = process.platform === "win32" ? "zip" : "tar.gz";
  const wanted = `nanobots_${version}_${GOOS}_${GOARCH}.${ext}`;
  const asset = (meta.assets || []).find((a) => a.name === wanted);
  if (!asset) {
    const names = (meta.assets || []).map((a) => a.name).join(", ");
    throw new Error(`release ${meta.tag_name} has no ${wanted}. It has: ${names}`);
  }

  fs.mkdirSync(cacheDir, { recursive: true });
  const archive = path.join(cacheDir, wanted);
  fs.writeFileSync(archive, await get(asset.browser_download_url));

  // tar and unzip rather than a dependency: both ship with every supported
  // OS, and this file having no node_modules is the point.
  const unpack =
    ext === "zip"
      ? spawnSync("unzip", ["-o", archive, "nanobots.exe", "-d", cacheDir], { stdio: "inherit" })
      : spawnSync("tar", ["-xzf", archive, "-C", cacheDir, "nanobots"], { stdio: "inherit" });
  if (unpack.status !== 0) throw new Error(`could not unpack ${archive}`);

  fs.chmodSync(exe, 0o755);
  fs.rmSync(archive, { force: true });
}

(async () => {
  try {
    if (!fs.existsSync(exe)) await download();
  } catch (err) {
    console.error(`nanobots: ${err.message}`);
    console.error(`Install another way: https://github.com/${REPO}#install`);
    process.exit(1);
  }
  const run = spawnSync(exe, process.argv.slice(2), { stdio: "inherit" });
  process.exit(run.status === null ? 1 : run.status);
})();
