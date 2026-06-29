#!/usr/bin/env node
const { execFileSync, spawnSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");
const readline = require("readline");

const PKG = "@lark-project/meegle";
const SKILLS_REPO = "larksuite/meegle-cli";
const DEFAULT_HOST = "meegle.com";
const PRESET_HOSTS = [
  { label: "Feishu Project (project.feishu.cn)", value: "project.feishu.cn" },
  { label: "Meegle (meegle.com)", value: "meegle.com" },
];
const isWindows = process.platform === "win32";

const messages = {
  en: {
    setup: "Setting up Meegle CLI...",
    language: "Select language",
    installing: "Installing %s globally...",
    upgrading: "Upgrading %s (v%s -> v%s)...",
    installedSkip: "Already installed globally (v%s). Skipped",
    installed: "CLI installed globally",
    skills: "Installing AI Agent Skill...",
    skillsDone: "AI Agent Skill installed",
    hostExisting: "Found existing host: %s. Use it?",
    hostSelect: "Select Meegle site",
    hostCustom: "Custom domain",
    hostInput: "Enter custom domain, for example project.feishu.cn: ",
    hostConfigured: "Host configured: %s",
    authSkip: "Skipped authorization. Run later: meegle auth login",
    authAlready: "Already authenticated. Skipped authorization",
    authConfirm: "Log in now?",
    authBrowser: "Starting browser OAuth login...",
    authDevice: "Starting Device Code login...",
    done: "Setup complete. Try: meegle mywork todo --action this_week --page-num 1",
    nonTtyHost: "No TTY detected and no --host was provided. Using default host: %s",
  },
  zh: {
    setup: "正在设置 Meegle CLI...",
    language: "请选择语言",
    installing: "正在全局安装 %s...",
    upgrading: "正在升级 %s (v%s -> v%s)...",
    installedSkip: "已全局安装 (v%s)，跳过",
    installed: "CLI 已全局安装",
    skills: "正在安装 AI Agent Skill...",
    skillsDone: "AI Agent Skill 已安装",
    hostExisting: "发现已有 host: %s，继续使用？",
    hostSelect: "请选择 Meegle 站点",
    hostCustom: "自定义域名",
    hostInput: "请输入自定义域名，例如 project.feishu.cn: ",
    hostConfigured: "Host 已配置: %s",
    authSkip: "已跳过授权。后续可运行: meegle auth login",
    authAlready: "已登录，跳过授权",
    authConfirm: "现在登录？",
    authBrowser: "正在启动浏览器 OAuth 登录...",
    authDevice: "正在启动 Device Code 登录...",
    done: "安装完成。可以试试: meegle mywork todo --action this_week --page-num 1",
    nonTtyHost: "当前不是交互终端且未传 --host，将使用默认 host: %s",
  },
};

function fmt(template, ...values) {
  let i = 0;
  return template.replace(/%s/g, () => String(values[i++] ?? ""));
}

function parseArgs(argv) {
  const opts = {
    auth: true,
    skills: true,
    deviceCode: false,
    yes: false,
    host: "",
    lang: "",
  };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--host" && argv[i + 1]) {
      opts.host = argv[++i];
    } else if (arg.startsWith("--host=")) {
      opts.host = arg.slice("--host=".length);
    } else if (arg === "--lang" && argv[i + 1]) {
      opts.lang = argv[++i];
    } else if (arg.startsWith("--lang=")) {
      opts.lang = arg.slice("--lang=".length);
    } else if (arg === "--device-code") {
      opts.deviceCode = true;
    } else if (arg === "--no-auth") {
      opts.auth = false;
    } else if (arg === "--no-skills") {
      opts.skills = false;
    } else if (arg === "-y" || arg === "--yes") {
      opts.yes = true;
    } else if (arg === "-h" || arg === "--help") {
      printHelp();
      process.exit(0);
    }
  }
  if (opts.lang !== "zh" && opts.lang !== "en") {
    opts.lang = "";
  }
  return opts;
}

function printHelp() {
  console.log(`Usage: meegle install [options]

Set up Meegle CLI, AI Agent Skill, host configuration, and login.

Options:
  --host <host>       Configure a site host, e.g. project.feishu.cn
  --device-code       Use Device Code login instead of browser OAuth
  --no-auth           Skip login
  --no-skills         Skip AI Agent Skill installation
  --lang <en|zh>      Select wizard language
  -y, --yes           Use defaults for non-essential prompts
  -h, --help          Show help`);
}

function cmdParts(cmd, args) {
  if (!isWindows) return { cmd, args };
  return { cmd: "cmd.exe", args: ["/c", cmd, ...args] };
}

function run(cmd, args, opts = {}) {
  const actual = cmdParts(cmd, args);
  const result = spawnSync(actual.cmd, actual.args, {
    stdio: "inherit",
    env: process.env,
    ...opts,
  });
  if (result.signal) process.kill(process.pid, result.signal);
  if (result.status !== 0) {
    throw new Error(`${cmd} ${args.join(" ")} failed with exit code ${result.status}`);
  }
}

function runSilent(cmd, args, opts = {}) {
  const actual = cmdParts(cmd, args);
  return execFileSync(actual.cmd, actual.args, {
    stdio: ["ignore", "pipe", "pipe"],
    encoding: "utf8",
    env: process.env,
    ...opts,
  });
}

function runCapture(cmd, args, opts = {}) {
  const actual = cmdParts(cmd, args);
  return spawnSync(actual.cmd, actual.args, {
    stdio: ["ignore", "pipe", "pipe"],
    encoding: "utf8",
    env: process.env,
    ...opts,
  });
}

function question(prompt) {
  const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
  return new Promise((resolve) => {
    rl.question(prompt, (answer) => {
      rl.close();
      resolve(answer.trim());
    });
  });
}

async function select(message, options) {
  console.log(message);
  options.forEach((opt, idx) => {
    console.log(`  ${idx + 1}. ${opt.label}`);
  });
  for (;;) {
    const answer = await question("> ");
    const idx = Number(answer);
    if (Number.isInteger(idx) && idx >= 1 && idx <= options.length) {
      return options[idx - 1].value;
    }
  }
}

async function confirm(message, defaultValue) {
  const suffix = defaultValue ? " [Y/n] " : " [y/N] ";
  const answer = (await question(message + suffix)).toLowerCase();
  if (!answer) return defaultValue;
  return answer === "y" || answer === "yes";
}

function semverLessThan(a, b) {
  const pa = String(a).replace(/-.*/, "").split(".").map(Number);
  const pb = String(b).replace(/-.*/, "").split(".").map(Number);
  for (let i = 0; i < 3; i += 1) {
    if ((pa[i] || 0) < (pb[i] || 0)) return true;
    if ((pa[i] || 0) > (pb[i] || 0)) return false;
  }
  return false;
}

function getGloballyInstalledVersion() {
  try {
    const out = runSilent("npm", ["list", "-g", PKG, "--depth=0"], { timeout: 15000 });
    const match = out.match(/@lark-project\/meegle@([^\s]+)/);
    return match ? match[1] : "unknown";
  } catch (_) {
    return null;
  }
}

function getLatestVersion() {
  try {
    const out = runSilent("npm", ["view", PKG, "version"], { timeout: 15000 }).trim();
    return /^\d+\.\d+\.\d+/.test(out) ? out : null;
  } catch (_) {
    return null;
  }
}

function whichMeegle() {
  try {
    const prefix = runSilent("npm", ["prefix", "-g"], { timeout: 15000 }).trim();
    const bin = isWindows ? path.join(prefix, "meegle.cmd") : path.join(prefix, "bin", "meegle");
    if (fs.existsSync(bin)) return bin;
  } catch (_) {
    // fall through
  }
  try {
    const cmd = isWindows ? "where" : "which";
    const out = runSilent(cmd, ["meegle"], { timeout: 15000 });
    return out.split(/\r?\n/).map((s) => s.trim()).find(Boolean) || null;
  } catch (_) {
    return null;
  }
}

function normalizeHost(raw) {
  const trimmed = String(raw || "").trim();
  if (!trimmed) return "";
  try {
    const withScheme = trimmed.includes("://") ? trimmed : `https://${trimmed}`;
    return new URL(withScheme).host;
  } catch (_) {
    return trimmed.replace(/^https?:\/\//, "").replace(/\/.*$/, "");
  }
}

async function selectLanguage(opts) {
  if (opts.lang) return opts.lang;
  const locale = (process.env.LANG || process.env.LANGUAGE || "").toLowerCase();
  if (!process.stdin.isTTY || opts.yes) return locale.includes("zh") ? "zh" : "en";
  return select("Select language / 请选择语言", [
    { label: "English", value: "en" },
    { label: "中文", value: "zh" },
  ]);
}

async function stepInstallGlobally(msg) {
  const installed = getGloballyInstalledVersion();
  const latest = getLatestVersion();
  const needsUpgrade = installed && latest && semverLessThan(installed, latest);
  if (installed && !needsUpgrade) {
    console.log(fmt(msg.installedSkip, installed));
    return;
  }
  console.log(needsUpgrade ? fmt(msg.upgrading, PKG, installed, latest) : fmt(msg.installing, PKG));
  run("npm", ["install", "-g", PKG], { timeout: 120000 });
  console.log(msg.installed);
}

async function stepInstallSkills(msg) {
  console.log(msg.skills);
  // `skills add` is idempotent (with `-y`): re-running on an already-installed
  // skill just upgrades / no-ops. We always run it instead of trying to detect
  // prior installs — name-substring detection mis-fired on sibling skills like
  // `meegle-plugin` and silently skipped the core skill.
  run("npm", ["exec", "--yes", "--package=skills", "--", "skills", "add", SKILLS_REPO, "-y", "-g"], { timeout: 120000 });
  console.log(msg.skillsDone);
}

function getConfiguredHost(meegleBin) {
  try {
    const out = runSilent(meegleBin, ["config", "get", "host"], { timeout: 15000 }).trim();
    if (!out || out === "(not set)") return "";
    return normalizeHost(out);
  } catch (_) {
    return "";
  }
}

async function resolveHost(opts, msg, meegleBin) {
  if (opts.host) return normalizeHost(opts.host);
  const existing = getConfiguredHost(meegleBin);
  if (existing) {
    if (!process.stdin.isTTY || opts.yes) return existing;
    if (await confirm(fmt(msg.hostExisting, existing), true)) return existing;
  }
  if (!process.stdin.isTTY || opts.yes) {
    console.log(fmt(msg.nonTtyHost, DEFAULT_HOST));
    return DEFAULT_HOST;
  }
  const selected = await select(msg.hostSelect, [
    ...PRESET_HOSTS,
    { label: msg.hostCustom, value: "__custom__" },
  ]);
  if (selected !== "__custom__") return selected;
  return normalizeHost(await question(msg.hostInput));
}

function configureHost(meegleBin, host, msg) {
  run(meegleBin, ["config", "set", "host", host], { timeout: 30000 });
  console.log(fmt(msg.hostConfigured, host));
}

function isAuthenticated(meegleBin) {
  const result = runCapture(meegleBin, ["auth", "status", "--format", "json"], { timeout: 15000 });
  const text = result.stdout || "";
  try {
    const parsed = JSON.parse(text);
    return !!parsed.authenticated;
  } catch (_) {
    return result.status === 0;
  }
}

async function stepAuth(meegleBin, host, opts, msg) {
  if (!opts.auth) {
    console.log(msg.authSkip);
    return;
  }
  if (isAuthenticated(meegleBin)) {
    console.log(msg.authAlready);
    return;
  }
  const useDeviceCode = opts.deviceCode || !process.stdin.isTTY;
  if (!useDeviceCode && !opts.yes) {
    const shouldLogin = await confirm(msg.authConfirm, true);
    if (!shouldLogin) {
      console.log(msg.authSkip);
      return;
    }
  }
  console.log(useDeviceCode ? msg.authDevice : msg.authBrowser);
  const args = ["auth", "login", "--host", host];
  if (useDeviceCode) args.push("--device-code");
  run(meegleBin, args, { timeout: 900000 });
}

async function main(argv = process.argv.slice(2)) {
  const opts = parseArgs(argv);
  const lang = await selectLanguage(opts);
  const msg = messages[lang];
  console.log(msg.setup);

  await stepInstallGlobally(msg);
  if (opts.skills) {
    await stepInstallSkills(msg);
  }

  const meegleBin = whichMeegle();
  if (!meegleBin) {
    throw new Error("meegle was installed but no global executable was found in PATH");
  }
  const host = await resolveHost(opts, msg, meegleBin);
  configureHost(meegleBin, host, msg);
  await stepAuth(meegleBin, host, opts, msg);

  console.log(msg.done);
}

if (require.main === module) {
  main().catch((err) => {
    console.error(err && err.message ? err.message : err);
    process.exit(1);
  });
}

module.exports = { main };
