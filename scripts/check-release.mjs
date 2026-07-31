#!/usr/bin/env node

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
if (process.env.GITHUB_ACTIONS !== "true" || process.env.RUNNER_ENVIRONMENT !== "github-hosted") {
  throw new Error("release must run on GitHub Actions");
}
if (process.env.JUMPOTP_REPOSITORY_VISIBILITY !== "public") {
  throw new Error("release repository must be public");
}

const tasks = await readFile(path.join(root, "openspec/changes/build-jumpotp-cli/tasks.md"), "utf8");
if (!/^- \[x\] 12\.4 /m.test(tasks)) {
  throw new Error("Trusted Publisher verification must be completed before tagging");
}

console.log(`Release checks passed for ${expectedTag}.`);
