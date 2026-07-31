#!/usr/bin/env node
"use strict";

const { spawn } = require("node:child_process");

const supported = {
  "darwin-arm64": "jumpotp-darwin-arm64",
  "darwin-x64": "jumpotp-darwin-x64",
  "linux-arm64": "jumpotp-linux-arm64",
  "linux-x64": "jumpotp-linux-x64"
};

const key = `${process.platform}-${process.arch}`;
const packageName = supported[key];

if (!packageName) {
  console.error(
    `JumpOTP does not support ${process.platform}/${process.arch} in v0.1. ` +
    "Supported platforms: darwin-arm64, darwin-x64, linux-arm64, linux-x64.\n" +
    "See https://github.com/nxxxsooo/jumpotp/releases for manual downloads."
  );
  process.exit(1);
}

let binary;
try {
  binary = require.resolve(`${packageName}/bin/jumpotp`);
} catch {
  console.error(
    `JumpOTP's optional native package ${packageName} is missing.\n` +
    "Repair: npm install -g jumpotp --include=optional\n" +
    "Fallback: https://github.com/nxxxsooo/jumpotp/releases"
  );
  process.exit(1);
}

const child = spawn(binary, process.argv.slice(2), {
  stdio: "inherit",
  windowsHide: true
});

for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"]) {
  process.on(signal, () => {
    if (!child.killed) child.kill(signal);
  });
}

child.on("error", (error) => {
  console.error(`JumpOTP failed to start its native binary: ${error.message}`);
  process.exit(1);
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code === null ? 1 : code);
});
