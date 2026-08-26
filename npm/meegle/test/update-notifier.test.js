const assert = require("assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { PassThrough } = require("stream");
const test = require("node:test");

const {
  CHECK_INTERVAL_MS,
  compareVersions,
  installLatest,
  maybeCheckForUpdates,
  parseReleaseNotes,
  renderUpdateNotice,
  selectUpdateAction,
  shouldRunUpdateCheck,
  updateAgentSkillBestEffort,
} = require("../bin/update-notifier.js");
const { installAgentSkill } = require("../bin/install-wizard.js");

const changelog = `# Changelog

## [Unreleased]

### Added

- Not released yet.

## [v1.2.0] - 2026-08-20

### Added

- Added a new command with \`--flag\` support.

### Changed

- Changed the default behavior; see [docs](https://example.com).

### Fixed

- This fix should not appear in feature highlights.

## [v1.1.0] - 2026-08-01

### Changed

- Improved an existing workflow.

## [v1.0.0] - 2026-07-01

Initial release.
`;

function fakeOutput() {
  return {
    isTTY: true,
    columns: 100,
    text: "",
    write(value) {
      this.text += String(value);
      return true;
    },
  };
}

test("compareVersions handles stable and prerelease versions", () => {
  assert.strictEqual(compareVersions("1.2.0", "1.1.9"), 1);
  assert.strictEqual(compareVersions("v1.2.0", "1.2.0"), 0);
  assert.strictEqual(compareVersions("1.2.0-beta.2", "1.2.0-beta.10"), -1);
  assert.strictEqual(compareVersions("1.2.0", "1.2.0-beta.10"), 1);
  assert.strictEqual(compareVersions("dev", "1.2.0"), null);
});

test("parseReleaseNotes returns Added and Changed items between versions", () => {
  const releases = parseReleaseNotes(changelog, "1.0.0", "1.2.0");
  assert.deepStrictEqual(releases.map((release) => release.version), ["1.2.0", "1.1.0"]);
  assert.deepStrictEqual(releases[0].items.map((item) => item.category), ["added", "changed"]);
  assert.ok(releases.every((release) => release.items.every((item) => !item.text.includes("fix"))));
});

test("renderUpdateNotice uses the feature template and sanitizes markdown", () => {
  const releases = parseReleaseNotes(changelog, "1.0.0", "1.2.0");
  const notice = renderUpdateNotice({
    currentVersion: "1.0.0",
    latestVersion: "1.2.0",
    releases,
    locale: "en",
    columns: 100,
  });
  assert.match(notice, /v1\.0\.0 → v1\.2\.0/);
  assert.match(notice, /v1\.2\.0/);
  assert.match(notice, /Added: Added a new command with --flag support\./);
  assert.match(notice, /Changed: Changed the default behavior; see docs\./);
  assert.doesNotMatch(notice, /https:\/\/example\.com/);
});

test("shouldRunUpdateCheck protects non-interactive and completion invocations", () => {
  const tty = { isTTY: true };
  const interactive = { input: tty, output: tty, stdout: tty };
  assert.strictEqual(shouldRunUpdateCheck({ args: ["workitem", "get"], ...interactive, env: {} }), true);
  assert.strictEqual(shouldRunUpdateCheck({ args: ["install"], ...interactive, env: {} }), false);
  assert.strictEqual(shouldRunUpdateCheck({ args: ["completion", "zsh"], ...interactive, env: {} }), false);
  assert.strictEqual(shouldRunUpdateCheck({ args: ["__complete"], ...interactive, env: {} }), false);
  assert.strictEqual(shouldRunUpdateCheck({ args: ["version"], input: {}, output: tty, stdout: tty, env: {} }), false);
  assert.strictEqual(shouldRunUpdateCheck({ args: ["version"], input: tty, output: tty, stdout: {}, env: {} }), false);
  assert.strictEqual(shouldRunUpdateCheck({ args: ["version"], ...interactive, env: { CI: "1" } }), false);
  assert.strictEqual(shouldRunUpdateCheck({ args: ["version"], ...interactive, env: { MEEGLE_NO_UPDATE_CHECK: "1" } }), false);
});

function interactiveStreams() {
  const input = new PassThrough();
  input.isTTY = true;
  input.isRaw = false;
  input.setRawMode = (value) => { input.isRaw = value; };
  const output = new PassThrough();
  output.isTTY = true;
  let rendered = "";
  output.on("data", (chunk) => { rendered += chunk.toString(); });
  return { input, output, rendered: () => rendered };
}

test("selectUpdateAction defaults to immediate update on Enter", async () => {
  const streams = interactiveStreams();
  const selection = selectUpdateAction({ input: streams.input, output: streams.output, locale: "en" });
  setImmediate(() => streams.input.write("\r"));
  assert.strictEqual(await selection, "update");
  assert.match(streams.rendered(), /❯ Update now \(recommended\)/);
});

test("selectUpdateAction supports down-arrow selection", async () => {
  const streams = interactiveStreams();
  const selection = selectUpdateAction({ input: streams.input, output: streams.output, locale: "en" });
  setImmediate(() => streams.input.write("\u001b[B\r"));
  assert.strictEqual(await selection, "later");
  assert.match(streams.rendered(), /Update now \(recommended\)/);
  assert.match(streams.rendered(), /Remind me later/);
});

test("installAgentSkill is strict for the full wizard and best-effort for updates", () => {
  const failure = () => { throw new Error("skills unavailable"); };
  assert.throws(() => installAgentSkill({ runFn: failure }), /skills unavailable/);
  assert.strictEqual(installAgentSkill({ bestEffort: true, runFn: failure }), false);
  assert.strictEqual(installAgentSkill({ bestEffort: true, runFn: () => {} }), true);
});

test("best-effort Skill failure does not turn the CLI update into a failure", () => {
  const output = fakeOutput();
  const updated = updateAgentSkillBestEffort({
    output,
    locale: "en",
    installerFn: () => { throw new Error("no suitable Skill installer"); },
  });
  assert.strictEqual(updated, false);
  assert.match(output.text, /CLI update is still complete/);
});

test("installLatest stays successful when the follow-up Skill upgrade fails", () => {
  const output = fakeOutput();
  const result = installLatest({
    latestVersion: "1.2.0",
    binName: "meegle-test",
    env: {},
    output,
    locale: "en",
    spawnFn: () => ({ status: 0, signal: null, error: null }),
    globalBinaryPathFn: () => "/new/meegle",
    skillInstallerFn: () => false,
  });
  assert.deepStrictEqual(result, {
    ok: true,
    binaryPath: "/new/meegle",
    skillUpdated: false,
  });
  assert.match(output.text, /Meegle CLI updated to v1\.2\.0/);
  assert.match(output.text, /Skill update was unavailable/);
  assert.match(output.text, /Continuing with your command/);
});

test("maybeCheckForUpdates defers for 24 hours without installing", async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "meegle-update-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const cacheFile = path.join(dir, "cache.json");
  const output = fakeOutput();
  const input = { isTTY: true };
  let installed = false;
  const now = 123456789;

  const result = await maybeCheckForUpdates({
    args: ["version"],
    currentVersion: "1.0.0",
    input,
    output,
    stdout: { isTTY: true },
    env: {},
    now,
    cacheFile,
    getLatestVersionFn: () => "1.2.0",
    fetchChangelogFn: () => changelog,
    selectActionFn: () => "later",
    installFn: () => { installed = true; return { ok: true }; },
  });

  assert.strictEqual(result.status, "deferred");
  assert.strictEqual(installed, false);
  assert.match(output.text, /v1\.0\.0 → v1\.2\.0/);
  const cached = JSON.parse(fs.readFileSync(cacheFile, "utf8"));
  assert.strictEqual(cached.next_check_at, now + CHECK_INTERVAL_MS);
});

test("maybeCheckForUpdates installs the latest version and returns its binary", async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "meegle-update-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const result = await maybeCheckForUpdates({
    args: ["version"],
    currentVersion: "1.0.0",
    input: { isTTY: true },
    output: fakeOutput(),
    stdout: { isTTY: true },
    env: {},
    cacheFile: path.join(dir, "cache.json"),
    getLatestVersionFn: () => "1.2.0",
    fetchChangelogFn: () => changelog,
    selectActionFn: () => "update",
    installFn: () => ({ ok: true, binaryPath: "/new/meegle" }),
  });
  assert.deepStrictEqual(result, {
    status: "updated",
    updated: true,
    binaryPath: "/new/meegle",
  });
});

test("maybeCheckForUpdates honors a fresh cache without querying npm", async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "meegle-update-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const cacheFile = path.join(dir, "cache.json");
  fs.writeFileSync(cacheFile, JSON.stringify({
    schema_version: 1,
    current_version: "1.0.0",
    next_check_at: 2000,
  }));
  const result = await maybeCheckForUpdates({
    args: ["version"],
    currentVersion: "1.0.0",
    input: { isTTY: true },
    output: fakeOutput(),
    stdout: { isTTY: true },
    env: {},
    now: 1000,
    cacheFile,
    getLatestVersionFn: () => { throw new Error("must not be called"); },
  });
  assert.strictEqual(result.status, "cached");
});
