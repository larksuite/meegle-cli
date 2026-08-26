#!/usr/bin/env node
const { spawnSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { maybeCheckForUpdates } = require("./update-notifier.js");
const packageJSON = require("../package.json");

const SUPPORTED = [
  "darwin-arm64", "darwin-x64",
  "linux-arm64", "linux-x64",
  "win32-arm64", "win32-x64",
];

const platform = os.platform();
const arch = os.arch();
const ext = platform === "win32" ? ".exe" : "";
const binName = `meegle-${platform}-${arch}${ext}`;
const binPath = path.join(__dirname, binName);

async function main(args = process.argv.slice(2)) {
  if (args[0] === "install") {
    await require("./install-wizard.js").main(args.slice(1));
    return 0;
  }

  try {
    fs.accessSync(binPath, fs.constants.X_OK);
  } catch {
    const detected = `${platform}-${arch}`;
    const isSupported = SUPPORTED.includes(detected);
    console.error(
      isSupported
        ? `meegle binary is missing or not executable.\n` +
          `Detected platform: ${detected}\n` +
          `Expected binary at: ${binPath}\n` +
          `Try reinstalling: npm i -g @lark-project/meegle\n` +
          `If the problem persists, please file an issue with the output of: node -v && npm -v`
        : `Unsupported platform: ${detected}\n` +
          `Supported platforms: ${SUPPORTED.join(", ")}`
    );
    return 1;
  }

  let commandBinPath = binPath;
  try {
    const update = await maybeCheckForUpdates({
      args,
      currentVersion: packageJSON.version,
      binName,
    });
    if (update.updated && update.binaryPath) commandBinPath = update.binaryPath;
  } catch (err) {
    if (err && err.code === "UPDATE_PROMPT_INTERRUPTED") {
      process.kill(process.pid, "SIGINT");
      return 130;
    }
    // The update notifier is best-effort. A registry, GitHub, cache, or prompt
    // failure must never prevent the user's actual CLI command from running.
  }

  const result = spawnSync(commandBinPath, args, { stdio: "inherit" });

  // Re-raise the signal so parent shells see the real cause (e.g. 130 for SIGINT)
  // instead of a generic exit 1.
  if (result.signal) {
    process.kill(process.pid, result.signal);
  }
  return result.status ?? 1;
}

if (require.main === module) {
  main().then((status) => {
    process.exit(status);
  }).catch((err) => {
    console.error("Unexpected meegle error:", err && err.message ? err.message : err);
    process.exit(1);
  });
}

module.exports = { main };
