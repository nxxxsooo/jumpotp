#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const version = (await readFile(path.join(root, "VERSION"), "utf8")).trim();
const expectedTag = `v${version}`;
const repository = process.env.GITHUB_REPOSITORY;
const ref = process.env.GITHUB_REF_NAME;

if (repository !== "nxxxsooo/jumpotp") throw new Error(`unexpected repository: ${repository}`);
if (ref !== expectedTag) throw new Error(`tag ${ref} does not match ${expectedTag}`);
if (process.env.GITHUB_ACTIONS !== "true" || !process.env.RUNNER_ENVIRONMENT) {
  throw new Error("release must run on GitHub Actions");
}

const packages = [
  "jumpotp",
  "jumpotp-darwin-arm64",
  "jumpotp-darwin-x64",
  "jumpotp-linux-arm64",
  "jumpotp-linux-x64"
];

for (const packageName of packages) {
  const trust = execFileSync("npm", ["trust", "list", packageName, "--json"], { encoding: "utf8" });
  if (!trust.includes("nxxxsooo/jumpotp") || !trust.includes("release.yml")) {
    throw new Error(`${packageName}: Trusted Publisher does not match this release workflow`);
  }
}

console.log(`Release checks passed for ${expectedTag}.`);
