#!/usr/bin/env node

import { execFileSync } from "node:child_process";

const packages = [
  "jumpotp",
  "jumpotp-darwin-arm64",
  "jumpotp-darwin-x64",
  "jumpotp-linux-arm64",
  "jumpotp-linux-x64"
];

for (const packageName of packages) {
  let result;
  try {
    result = execFileSync("npm", ["trust", "list", packageName, "--json"], {
      encoding: "utf8",
      stdio: ["inherit", "pipe", "inherit"]
    });
  } catch {
    throw new Error(`${packageName}: authenticated Trusted Publisher lookup failed`);
  }

  const parsed = JSON.parse(result);
  const trusts = Array.isArray(parsed) ? parsed : [parsed];
  if (trusts.length !== 1) {
    throw new Error(`${packageName}: expected exactly one Trusted Publisher`);
  }
  const trust = trusts[0];
  const permissions = [...(trust.permissions || [])].sort();
  if (trust.type !== "github" ||
      trust.file !== "release.yml" ||
      trust.repository !== "nxxxsooo/jumpotp" ||
      trust.environment !== "npm" ||
      permissions.length !== 1 || permissions[0] !== "createPackage") {
    throw new Error(`${packageName}: Trusted Publisher does not match the release contract`);
  }
  console.log(`${packageName}: Trusted Publisher verified`);
}
