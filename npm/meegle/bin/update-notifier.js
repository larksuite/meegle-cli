#!/usr/bin/env node
const { execFileSync, spawnSync } = require("child_process");
const fs = require("fs");
const https = require("https");
const os = require("os");
const path = require("path");
const readline = require("readline");

const PKG = "@lark-project/meegle";
const CHANGELOG_URL = "https://github.com/larksuite/meegle-cli/blob/main/CHANGELOG.md#changed";
const CHANGELOG_RAW_URL = "https://raw.githubusercontent.com/larksuite/meegle-cli/main/CHANGELOG.md";
const CACHE_SCHEMA_VERSION = 1;
const CHECK_INTERVAL_MS = 24 * 60 * 60 * 1000;
const FAILURE_RETRY_MS = 60 * 60 * 1000;
const NETWORK_TIMEOUT_MS = 5000;
const MAX_CHANGELOG_BYTES = 1024 * 1024;
const MAX_FEATURE_ITEMS = 8;

const messages = {
  en: {
    title: "Meegle CLI update available",
    whatsNew: "What's new",
    added: "Added",
    changed: "Changed",
    maintenance: "This update mainly contains fixes and maintenance changes.",
    omitted: "... and %s more feature changes",
    changelog: "Full changelog",
    updateNow: "Update now (recommended)",
    later: "Remind me later",
    hint: "Use ↑/↓ to select, then press Enter",
    updating: "Updating @lark-project/meegle to v%s...\n",
    updated: "✓ Meegle CLI updated to v%s.\n",
    skillUpdating: "Updating the Meegle Agent Skill (best effort)...\n",
    skillUpdated: "✓ Meegle Agent Skill updated.\n",
    skillSkipped: "! Meegle Agent Skill update was unavailable; the CLI update is still complete.\n",
    continuing: "Continuing with your command...\n\n",
    updateFailed: "Could not update automatically. Continue with the current version and retry later:\n  npm install -g @lark-project/meegle@latest\n\n",
  },
  zh: {
    title: "Meegle CLI 有新版本",
    whatsNew: "功能上新",
    added: "新增",
    changed: "变化",
    maintenance: "本次更新以问题修复和维护性改进为主。",
    omitted: "……另有 %s 项功能变化",
    changelog: "完整更新日志",
    updateNow: "立即更新（推荐）",
    later: "稍后提醒",
    hint: "使用 ↑/↓ 选择，按 Enter 确认",
    updating: "正在将 @lark-project/meegle 更新到 v%s...\n",
    updated: "✓ Meegle CLI 已更新到 v%s。\n",
    skillUpdating: "正在尝试更新 Meegle Agent Skill（失败不影响 CLI 更新）...\n",
    skillUpdated: "✓ Meegle Agent Skill 已更新。\n",
    skillSkipped: "! 当前环境无法更新 Meegle Agent Skill；CLI 已成功更新。\n",
    continuing: "将继续执行当前命令。\n\n",
    updateFailed: "自动更新失败，将继续使用当前版本。你可以稍后手动执行：\n  npm install -g @lark-project/meegle@latest\n\n",
  },
};

function fmt(template, ...values) {
  let index = 0;
  return template.replace(/%s/g, () => String(values[index++] ?? ""));
}

function localeFromEnv(env = process.env) {
  const locale = [env.LC_ALL, env.LC_MESSAGES, env.LANG, env.LANGUAGE]
    .find((value) => typeof value === "string" && value.trim() !== "") || "";
  return locale.toLowerCase().startsWith("zh") ? "zh" : "en";
}

function parseSemver(raw) {
  const match = String(raw || "").trim().match(
    /^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/,
  );
  if (!match) return null;
  return {
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
    prerelease: match[4] ? match[4].split(".") : [],
  };
}

function comparePrerelease(left, right) {
  if (left.length === 0 && right.length === 0) return 0;
  if (left.length === 0) return 1;
  if (right.length === 0) return -1;
  const count = Math.max(left.length, right.length);
  for (let index = 0; index < count; index += 1) {
    if (left[index] === undefined) return -1;
    if (right[index] === undefined) return 1;
    if (left[index] === right[index]) continue;
    const leftNumber = /^\d+$/.test(left[index]) ? Number(left[index]) : null;
    const rightNumber = /^\d+$/.test(right[index]) ? Number(right[index]) : null;
    if (leftNumber !== null && rightNumber !== null) return leftNumber < rightNumber ? -1 : 1;
    if (leftNumber !== null) return -1;
    if (rightNumber !== null) return 1;
    return left[index].localeCompare(right[index]) < 0 ? -1 : 1;
  }
  return 0;
}

function compareVersions(leftRaw, rightRaw) {
  const left = parseSemver(leftRaw);
  const right = parseSemver(rightRaw);
  if (!left || !right) return null;
  for (const field of ["major", "minor", "patch"]) {
    if (left[field] !== right[field]) return left[field] < right[field] ? -1 : 1;
  }
  return comparePrerelease(left.prerelease, right.prerelease);
}

function isNewerVersion(candidate, current) {
  return compareVersions(candidate, current) === 1;
}

function parseFeatureItems(section) {
  const items = [];
  let category = "";
  let activeItem = null;
  for (const line of section.split(/\r?\n/)) {
    const heading = line.match(/^###\s+(Added|Changed)\s*$/i);
    if (heading) {
      category = heading[1].toLowerCase();
      activeItem = null;
      continue;
    }
    if (/^###\s+/.test(line)) {
      category = "";
      activeItem = null;
      continue;
    }
    if (!category) continue;
    const bullet = line.match(/^-\s+(.+)/);
    if (bullet) {
      activeItem = { category, text: bullet[1].trim() };
      items.push(activeItem);
      continue;
    }
    if (activeItem && /^\s{2,}\S/.test(line)) {
      activeItem.text += ` ${line.trim()}`;
    }
  }
  return items;
}

function parseReleaseNotes(markdown, currentVersion, latestVersion) {
  const source = String(markdown || "");
  const headingPattern = /^## \[v?([^\]]+)\](?:\s+-\s+.*)?\s*$/gm;
  const headings = Array.from(source.matchAll(headingPattern));
  const releases = [];
  for (let index = 0; index < headings.length; index += 1) {
    const version = headings[index][1].trim();
    const newerThanCurrent = compareVersions(version, currentVersion);
    const noNewerThanLatest = compareVersions(version, latestVersion);
    if (newerThanCurrent !== 1 || noNewerThanLatest === null || noNewerThanLatest > 0) continue;
    const start = headings[index].index + headings[index][0].length;
    const end = index + 1 < headings.length ? headings[index + 1].index : source.length;
    releases.push({ version, items: parseFeatureItems(source.slice(start, end)) });
  }
  releases.sort((left, right) => compareVersions(right.version, left.version) || 0);
  return releases;
}

function sanitizeMarkdown(text) {
  return String(text || "")
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/<[^>]+>/g, "")
    .replace(/[`*_~]/g, "")
    .replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

function truncate(text, maxLength) {
  const chars = Array.from(text);
  if (chars.length <= maxLength) return text;
  return `${chars.slice(0, Math.max(1, maxLength - 1)).join("")}…`;
}

function renderUpdateNotice({ currentVersion, latestVersion, releases, locale = "en", columns = 100 }) {
  const msg = messages[locale] || messages.en;
  const lines = [
    "",
    `✨ ${msg.title}: v${currentVersion} → v${latestVersion}`,
    "",
    `${msg.whatsNew}:`,
  ];
  const allItems = releases.flatMap((release) =>
    release.items.map((item) => ({ ...item, version: release.version })),
  );
  const maxTextLength = Math.max(60, Math.min(180, Number(columns || 100) - 18));
  let displayed = 0;
  let previousVersion = "";
  for (const item of allItems.slice(0, MAX_FEATURE_ITEMS)) {
    if (item.version !== previousVersion) {
      lines.push(`  v${item.version}`);
      previousVersion = item.version;
    }
    const category = item.category === "added" ? msg.added : msg.changed;
    lines.push(`    • ${category}: ${truncate(sanitizeMarkdown(item.text), maxTextLength)}`);
    displayed += 1;
  }
  if (displayed === 0) lines.push(`  • ${msg.maintenance}`);
  if (allItems.length > displayed) lines.push(`  ${fmt(msg.omitted, allItems.length - displayed)}`);
  lines.push("", `${msg.changelog}: ${CHANGELOG_URL}`, "");
  return `${lines.join("\n")}\n`;
}

function renderChoices(output, msg, selected, rerender) {
  if (rerender) {
    readline.cursorTo(output, 0);
    readline.moveCursor(output, 0, -2);
    readline.clearScreenDown(output);
  }
  const marker = (index) => (selected === index ? "❯" : " ");
  output.write([
    `${marker(0)} ${msg.updateNow}`,
    `${marker(1)} ${msg.later}`,
    `  ${msg.hint}`,
  ].join("\n"));
}

function selectUpdateAction({ input = process.stdin, output = process.stderr, locale = "en" } = {}) {
  const msg = messages[locale] || messages.en;
  if (!input.isTTY || !output.isTTY) return Promise.resolve("later");
  readline.emitKeypressEvents(input);
  const wasRaw = Boolean(input.isRaw);
  const wasPaused = typeof input.isPaused === "function" ? input.isPaused() : true;
  if (typeof input.setRawMode === "function") input.setRawMode(true);
  input.resume();
  let selected = 0;
  let rendered = false;

  return new Promise((resolve, reject) => {
    function cleanup() {
      input.removeListener("keypress", onKeypress);
      if (typeof input.setRawMode === "function" && !wasRaw) input.setRawMode(false);
      if (wasPaused && typeof input.pause === "function") input.pause();
      output.write("\n");
    }

    function onKeypress(_value, key = {}) {
      if (key.ctrl && key.name === "c") {
        cleanup();
        const err = new Error("update prompt interrupted");
        err.code = "UPDATE_PROMPT_INTERRUPTED";
        reject(err);
        return;
      }
      if (key.name === "up" || key.name === "down") {
        selected = selected === 0 ? 1 : 0;
        renderChoices(output, msg, selected, rendered);
        rendered = true;
        return;
      }
      if (key.name === "return" || key.name === "enter") {
        cleanup();
        resolve(selected === 0 ? "update" : "later");
      }
    }

    input.on("keypress", onKeypress);
    renderChoices(output, msg, selected, rendered);
    rendered = true;
  });
}

function npmInvocation(args, env = process.env) {
  if (process.platform !== "win32") return { command: "npm", args };
  return {
    command: env.ComSpec || env.COMSPEC || "cmd.exe",
    args: ["/d", "/s", "/c", "npm", ...args],
  };
}

function getLatestVersion({ env = process.env } = {}) {
  const invocation = npmInvocation(["view", PKG, "version"], env);
  const value = execFileSync(invocation.command, invocation.args, {
    encoding: "utf8",
    env,
    stdio: ["ignore", "pipe", "pipe"],
    timeout: NETWORK_TIMEOUT_MS,
  }).trim();
  return parseSemver(value) ? value.replace(/^v/, "") : null;
}

function fetchText(url, redirectsLeft = 3) {
  return new Promise((resolve, reject) => {
    const request = https.get(url, {
      headers: {
        Accept: "text/plain",
        "User-Agent": "meegle-cli-update-notifier",
      },
    }, (response) => {
      const status = response.statusCode || 0;
      if (status >= 300 && status < 400 && response.headers.location && redirectsLeft > 0) {
        response.resume();
        const redirectURL = new URL(response.headers.location, url);
        if (redirectURL.protocol !== "https:") {
          reject(new Error("refusing non-HTTPS changelog redirect"));
          return;
        }
        fetchText(redirectURL.toString(), redirectsLeft - 1).then(resolve, reject);
        return;
      }
      if (status !== 200) {
        response.resume();
        reject(new Error(`changelog request failed with HTTP ${status}`));
        return;
      }
      const chunks = [];
      let size = 0;
      response.on("data", (chunk) => {
        size += chunk.length;
        if (size > MAX_CHANGELOG_BYTES) {
          request.destroy(new Error("changelog response is too large"));
          return;
        }
        chunks.push(chunk);
      });
      response.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
      response.on("error", reject);
    });
    request.setTimeout(NETWORK_TIMEOUT_MS, () => request.destroy(new Error("changelog request timed out")));
    request.on("error", reject);
  });
}

function fetchChangelog() {
  return fetchText(CHANGELOG_RAW_URL);
}

function defaultCacheFile(env = process.env) {
  if (env.MEEGLE_UPDATE_CACHE_FILE) return env.MEEGLE_UPDATE_CACHE_FILE;
  return path.join(os.homedir(), ".meegle", "cache", "update-notifier.json");
}

function readCache(cacheFile) {
  try {
    const parsed = JSON.parse(fs.readFileSync(cacheFile, "utf8"));
    return parsed && parsed.schema_version === CACHE_SCHEMA_VERSION ? parsed : {};
  } catch (_) {
    return {};
  }
}

function writeCache(cacheFile, value) {
  try {
    fs.mkdirSync(path.dirname(cacheFile), { recursive: true, mode: 0o700 });
    fs.writeFileSync(cacheFile, `${JSON.stringify({ schema_version: CACHE_SCHEMA_VERSION, ...value }, null, 2)}\n`, {
      mode: 0o600,
    });
  } catch (_) {
    // Update checks are best-effort and must never block a CLI command.
  }
}

function truthyEnv(value) {
  const normalized = String(value || "").trim().toLowerCase();
  return normalized !== "" && normalized !== "0" && normalized !== "false" && normalized !== "no";
}

function shouldRunUpdateCheck({
  args = [],
  input = process.stdin,
  output = process.stderr,
  stdout = process.stdout,
  env = process.env,
} = {}) {
  if (!input.isTTY || !output.isTTY || !stdout.isTTY) return false;
  if (truthyEnv(env.CI) || truthyEnv(env.MEEGLE_NO_UPDATE_CHECK)) return false;
  const command = args[0] || "";
  return command !== "install" && command !== "completion" && !command.startsWith("__complete");
}

function getGlobalBinaryPath(binName, env = process.env) {
  try {
    const invocation = npmInvocation(["root", "-g"], env);
    const root = execFileSync(invocation.command, invocation.args, {
      encoding: "utf8",
      env,
      stdio: ["ignore", "pipe", "pipe"],
      timeout: NETWORK_TIMEOUT_MS,
    }).trim();
    const candidate = path.join(root, ...PKG.split("/"), "bin", binName);
    fs.accessSync(candidate, process.platform === "win32" ? fs.constants.F_OK : fs.constants.X_OK);
    return candidate;
  } catch (_) {
    return "";
  }
}

function updateAgentSkillBestEffort({ output = process.stderr, locale = "en", installerFn } = {}) {
  const msg = messages[locale] || messages.en;
  output.write(msg.skillUpdating);
  let updated = false;
  try {
    const install = installerFn || require("./install-wizard.js").installAgentSkill;
    updated = install({ bestEffort: true }) !== false;
  } catch (_) {
    updated = false;
  }
  output.write(updated ? msg.skillUpdated : msg.skillSkipped);
  return updated;
}

function installLatest({
  latestVersion,
  binName,
  env = process.env,
  output = process.stderr,
  locale = "en",
  skillInstallerFn,
  spawnFn = spawnSync,
  globalBinaryPathFn = getGlobalBinaryPath,
}) {
  const msg = messages[locale] || messages.en;
  output.write(fmt(msg.updating, latestVersion));
  const invocation = npmInvocation(["install", "-g", `${PKG}@latest`], env);
  const result = spawnFn(invocation.command, invocation.args, {
    env,
    stdio: "inherit",
    timeout: 120000,
  });
  if (result.signal) process.kill(process.pid, result.signal);
  if (result.error || result.status !== 0) {
    output.write(msg.updateFailed);
    return { ok: false, binaryPath: "" };
  }
  output.write(fmt(msg.updated, latestVersion));
  const skillUpdated = updateAgentSkillBestEffort({ output, locale, installerFn: skillInstallerFn });
  output.write(msg.continuing);
  return { ok: true, binaryPath: globalBinaryPathFn(binName, env), skillUpdated };
}

async function maybeCheckForUpdates(options = {}) {
  const args = options.args || [];
  const input = options.input || process.stdin;
  const output = options.output || process.stderr;
  const stdout = options.stdout || process.stdout;
  const env = options.env || process.env;
  const currentVersion = options.currentVersion;
  const binName = options.binName || "";
  const now = options.now === undefined ? Date.now() : options.now;
  const locale = options.locale || localeFromEnv(env);
  const cacheFile = options.cacheFile || defaultCacheFile(env);
  if (!shouldRunUpdateCheck({ args, input, output, stdout, env }) || !parseSemver(currentVersion)) {
    return { status: "skipped", updated: false, binaryPath: "" };
  }

  const cache = readCache(cacheFile);
  if (cache.current_version === currentVersion && Number(cache.next_check_at || 0) > now) {
    return { status: "cached", updated: false, binaryPath: "" };
  }

  const latestVersionFn = options.getLatestVersionFn || getLatestVersion;
  let latestVersion;
  try {
    latestVersion = await latestVersionFn({ env });
  } catch (_) {
    latestVersion = null;
  }
  if (!latestVersion || !parseSemver(latestVersion)) {
    writeCache(cacheFile, {
      current_version: currentVersion,
      checked_at: now,
      next_check_at: now + FAILURE_RETRY_MS,
    });
    return { status: "unavailable", updated: false, binaryPath: "" };
  }
  if (!isNewerVersion(latestVersion, currentVersion)) {
    writeCache(cacheFile, {
      current_version: currentVersion,
      latest_version: latestVersion,
      checked_at: now,
      next_check_at: now + CHECK_INTERVAL_MS,
    });
    return { status: "current", updated: false, binaryPath: "" };
  }

  const fetchChangelogFn = options.fetchChangelogFn || fetchChangelog;
  let changelog = "";
  try {
    changelog = await fetchChangelogFn();
  } catch (_) {
    // Version discovery still succeeded, so show the update with a link even
    // when GitHub is temporarily unreachable.
  }
  const releases = parseReleaseNotes(changelog, currentVersion, latestVersion);
  output.write(renderUpdateNotice({
    currentVersion,
    latestVersion,
    releases,
    locale,
    columns: output.columns,
  }));

  const selectActionFn = options.selectActionFn || selectUpdateAction;
  const action = await selectActionFn({ input, output, locale });
  if (action !== "update") {
    writeCache(cacheFile, {
      current_version: currentVersion,
      latest_version: latestVersion,
      checked_at: now,
      next_check_at: now + CHECK_INTERVAL_MS,
      deferred_at: now,
    });
    return { status: "deferred", updated: false, binaryPath: "" };
  }

  const installFn = options.installFn || installLatest;
  const installResult = await installFn({ latestVersion, binName, env, output, locale });
  const ok = installResult === true || Boolean(installResult && installResult.ok);
  writeCache(cacheFile, {
    current_version: ok ? latestVersion : currentVersion,
    latest_version: latestVersion,
    checked_at: now,
    next_check_at: now + (ok ? CHECK_INTERVAL_MS : FAILURE_RETRY_MS),
  });
  return {
    status: ok ? "updated" : "update-failed",
    updated: ok,
    binaryPath: ok && installResult.binaryPath ? installResult.binaryPath : "",
  };
}

module.exports = {
  CHANGELOG_URL,
  CHECK_INTERVAL_MS,
  compareVersions,
  defaultCacheFile,
  getLatestVersion,
  installLatest,
  isNewerVersion,
  localeFromEnv,
  maybeCheckForUpdates,
  parseReleaseNotes,
  renderUpdateNotice,
  selectUpdateAction,
  shouldRunUpdateCheck,
  updateAgentSkillBestEffort,
};
