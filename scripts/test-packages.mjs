#!/usr/bin/env node

import { execFileSync, spawnSync } from "node:child_process";
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const outputDir = path.join(root, "dist", "npm");
const isolatedNpmrc = path.join(outputDir, "test.npmrc");
const npmEnv = {
  ...process.env,
  npm_config_offline: "true",
  npm_config_cache: path.join(outputDir, "cache"),
  npm_config_userconfig: isolatedNpmrc
};
delete npmEnv.npm_config_allow_scripts;
delete npmEnv.NPM_CONFIG_ALLOW_SCRIPTS;
await rm(outputDir, { recursive: true, force: true });
await mkdir(outputDir, { recursive: true });
await writeFile(isolatedNpmrc, "ignore-scripts=true\n", { mode: 0o600 });

const packageDirs = [
  path.join(root, "packages/npm/jumpotp"),
  path.join(root, "packages/npm/platform/darwin-arm64"),
  path.join(root, "packages/npm/platform/darwin-x64"),
  path.join(root, "packages/npm/platform/linux-arm64"),
  path.join(root, "packages/npm/platform/linux-x64")
];

const tarballs = new Map();
for (const packageDir of packageDirs) {
  const result = JSON.parse(execFileSync("npm", ["pack", "--json", "--pack-destination", outputDir], {
    cwd: packageDir,
    encoding: "utf8",
    env: npmEnv
  }))[0];
  const allowed = new Set(["package.json", "README.md", "bin/jumpotp", "bin/jumpotp.js"]);
  for (const file of result.files) {
    if (!allowed.has(file.path)) throw new Error(`${result.name}: unexpected packed file ${file.path}`);
  }
  if (result.size > 50 * 1024 * 1024) throw new Error(`${result.name}: tarball is too large`);
  tarballs.set(result.name, path.join(outputDir, result.filename));
}

const arch = process.arch === "x64" ? "x64" : process.arch;
const platformName = `jumpotp-${process.platform}-${arch}`;
if (!tarballs.has(platformName)) throw new Error(`unsupported test platform ${process.platform}/${process.arch}`);

const consumer = await mkdtemp(path.join(os.tmpdir(), "jumpotp-consumer-"));
try {
  await writeFile(path.join(consumer, "package.json"), JSON.stringify({ name: "jumpotp-consumer", private: true }, null, 2));
  execFileSync("npm", ["install", "--ignore-scripts", "--omit=optional", tarballs.get("jumpotp")], { cwd: consumer, stdio: "pipe", env: npmEnv });
  const missing = spawnSync(path.join(consumer, "node_modules/.bin/jumpotp"), ["version", "--json"], { cwd: consumer, encoding: "utf8" });
  if (missing.status === 0 || !missing.stderr.includes("optional native package")) {
    throw new Error("launcher did not explain the missing optional dependency");
  }
  execFileSync("npm", ["install", "--ignore-scripts", "--omit=optional", tarballs.get(platformName)], { cwd: consumer, stdio: "pipe", env: npmEnv });
  const command = path.join(consumer, "node_modules/.bin/jumpotp");
  await chmod(command, 0o755);
  const version = JSON.parse(execFileSync(command, ["version", "--json"], { cwd: consumer, encoding: "utf8" }));
  const expected = (await readFile(path.join(root, "VERSION"), "utf8")).trim();
  if (version.name !== "jumpotp" || version.version !== expected) {
    throw new Error(`consumer version mismatch: ${JSON.stringify(version)}`);
  }
  execFileSync("npm", ["install", "--ignore-scripts", "--omit=optional", tarballs.get("jumpotp"), tarballs.get(platformName)], { cwd: consumer, stdio: "pipe", env: npmEnv });
  execFileSync("npm", ["uninstall", "jumpotp", platformName], { cwd: consumer, stdio: "pipe", env: npmEnv });
  if (spawnSync(command, ["version"]).status === 0) throw new Error("uninstall left an executable command");
  console.log(`Consumer lifecycle passed for ${platformName} at ${expected}.`);
} finally {
  await rm(consumer, { recursive: true, force: true });
}
