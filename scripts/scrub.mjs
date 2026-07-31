#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, readdir, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const syntheticCodes = new Set(["000000", "123456", "135790", "246810"]);
const findings = [];

const patterns = [
  ["private IPv4 address", /\b(?:10(?:\.\d{1,3}){3}|192\.168(?:\.\d{1,3}){2}|172\.(?:1[6-9]|2\d|3[01])(?:\.\d{1,3}){2})\b/g],
  ["private key", /BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY/g],
  ["API credential", /\b(?:sk|ghp|github_pat)-?[A-Za-z0-9_-]{16,}\b/g],
  ["session assignment", /\b(?:BW_SESSION|NODE_AUTH_TOKEN|NPM_TOKEN)\s*[:=]\s*["']?[A-Za-z0-9._-]{8,}/gi],
  ["non-example email", /\b[A-Z0-9._%+-]+@(?!example\.com\b|users\.noreply\.github\.com\b)[A-Z0-9.-]+\.[A-Z]{2,}\b/gi],
  ["non-synthetic item reference", /^\s*item:\s*(?!.*\bExample\b)(\S.+)$/gm]
];

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const files = new Set();
for (const file of gitFiles()) files.add(file);
for (const extra of ["dist", "packages/npm/platform"]) {
  for (const file of await walk(path.join(root, extra))) files.add(path.relative(root, file));
}

for (const relative of [...files].sort()) {
  if (relative.startsWith(".git/") || relative.includes("/node_modules/") || relative.includes("/cache/")) continue;
  const absolute = path.join(root, relative);
  let info;
  try {
    info = await stat(absolute);
  } catch {
    continue;
  }
  if (!info.isFile()) continue;
  if (/\.(?:tgz|tar\.gz|zip)$/.test(relative)) {
    await scanArchive(absolute, relative);
  } else {
    await scanFile(absolute, relative);
  }
}

await scanGitHistory();

const termsFile = process.env.JUMPOTP_FORBIDDEN_TERMS_FILE;
if (termsFile) {
  const terms = (await readFile(termsFile, "utf8")).split(/\r?\n/).map((value) => value.trim()).filter(Boolean);
  for (const relative of [...files].sort()) {
    const absolute = path.join(root, relative);
    let data;
    try {
      data = await readFile(absolute, "utf8");
    } catch {
      continue;
    }
    for (const term of terms) {
      if (data.toLowerCase().includes(term.toLowerCase())) findings.push({ file: relative, category: "private deployment term" });
    }
  }
}

if (findings.length > 0) {
  const unique = new Map(findings.map((finding) => [`${finding.file}: ${finding.category}`, finding]));
  for (const key of [...unique.keys()].sort()) console.error(`privacy scrub: ${key}`);
  process.exit(1);
}
console.log(`Privacy scrub passed for ${files.size} repository and generated files.`);

function gitFiles() {
  const output = execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "-z"], { cwd: root });
  return output.toString("utf8").split("\0").filter(Boolean);
}

async function scanGitHistory() {
  let objects;
  try {
    objects = execFileSync("git", ["rev-list", "--objects", "--all"], {
      cwd: root,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"]
    });
  } catch {
    // An empty local repository has no candidate history yet.
    return;
  }

  const directory = await mkdtemp(path.join(os.tmpdir(), "jumpotp-history-"));
  try {
    let index = 0;
    for (const line of objects.split(/\r?\n/).filter(Boolean)) {
      const separator = line.indexOf(" ");
      const object = separator < 0 ? line : line.slice(0, separator);
      const relative = separator < 0 ? object : line.slice(separator + 1);
      let type;
      try {
        type = execFileSync("git", ["cat-file", "-t", object], {
          cwd: root,
          encoding: "utf8",
          stdio: ["ignore", "pipe", "ignore"]
        }).trim();
      } catch {
        continue;
      }
      if (type !== "blob") continue;

      const data = execFileSync("git", ["cat-file", "blob", object], {
        cwd: root,
        encoding: null,
        maxBuffer: 128 * 1024 * 1024,
        stdio: ["ignore", "pipe", "ignore"]
      });
      const absolute = path.join(directory, String(index++));
      const source = `git-history:${relative}`;
      await writeFile(absolute, data);
      if (/\.(?:tgz|tar\.gz|zip)$/.test(relative)) {
        await scanArchive(absolute, source);
      } else {
        await scanFile(absolute, source);
      }
    }
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}

async function walk(directory) {
  const result = [];
  let entries;
  try {
    entries = await readdir(directory, { withFileTypes: true });
  } catch {
    return result;
  }
  for (const entry of entries) {
    if (entry.name === "node_modules" || entry.name === "cache") continue;
    const value = path.join(directory, entry.name);
    if (entry.isDirectory()) result.push(...await walk(value));
    else if (entry.isFile()) result.push(value);
  }
  return result;
}

async function scanFile(absolute, relative) {
  const data = await readFile(absolute);
  if (data.subarray(0, 8192).includes(0)) {
    try {
      scanText(execFileSync("strings", [absolute], { encoding: "utf8" }), relative + ":strings", true);
    } catch {
      findings.push({ file: relative, category: "uninspectable binary" });
    }
    return;
  }
  scanText(data.toString("utf8"), relative);
}

function scanText(text, source, binary = false) {
  for (const [category, expression] of patterns) {
    if (binary && !["private IPv4 address", "private key", "session assignment"].includes(category)) continue;
    expression.lastIndex = 0;
    if (expression.test(text)) findings.push({ file: source, category });
  }
  if (binary) return;
  for (const match of text.matchAll(/\b\d{6}\b/g)) {
    if (!syntheticCodes.has(match[0])) findings.push({ file: source, category: "unmarked six-digit value" });
  }
}

async function scanArchive(archive, relative) {
  const directory = await mkdtemp(path.join(os.tmpdir(), "jumpotp-scrub-"));
  try {
    if (relative.endsWith(".zip")) {
      execFileSync("unzip", ["-qq", archive, "-d", directory]);
    } else {
      execFileSync("tar", ["-xzf", archive, "-C", directory]);
    }
    for (const file of await walk(directory)) await scanFile(file, relative + ":" + path.relative(directory, file));
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}

function selfTest() {
  const start = findings.length;
  scanText("host: 192" + ".168.1.20", "private-ip");
  scanText("BW_" + "SESSION=syntheticsecret", "session");
  scanText("item: " + "Real Login", "item");
  scanText("code 987" + "654", "otp");
  const categories = new Set(findings.slice(start).map((finding) => finding.category));
  for (const expected of ["private IPv4 address", "session assignment", "non-synthetic item reference", "unmarked six-digit value"]) {
    if (!categories.has(expected)) throw new Error(`scrub self-test missed ${expected}`);
  }
  const beforeSafe = findings.length;
  scanText("host: app.example.com\nitem: Example Login\ncode 246810", "safe");
  if (findings.length !== beforeSafe) throw new Error("scrub rejected synthetic safe fixture");
  console.log("Privacy scrub self-test passed.");
}
