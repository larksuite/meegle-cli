#!/usr/bin/env node
const { spawnSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

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
  process.exit(1);
}

const result = spawnSync(binPath, process.argv.slice(2), { stdio: "inherit" });

// Re-raise the signal so parent shells see the real cause (e.g. 130 for SIGINT)
// instead of a generic exit 1.
if (result.signal) {
  process.kill(process.pid, result.signal);
}
process.exit(result.status ?? 1);
