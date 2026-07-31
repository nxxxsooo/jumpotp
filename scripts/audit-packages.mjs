#!/usr/bin/env node

import { access, readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const version = (await readFile(path.join(root, "VERSION"), "utf8")).trim();
const packages = [
  path.join(root, "packages/npm/jumpotp"),
  path.join(root, "packages/npm/platform/darwin-arm64"),
  path.join(root, "packages/npm/platform/darwin-x64"),
  path.join(root, "packages/npm/platform/linux-arm64"),
  path.join(root, "packages/npm/platform/linux-x64")
];
const forbiddenScripts = new Set(["preinstall", "install", "postinstall"]);

for (const packageDir of packages) {
  const manifest = JSON.parse(await readFile(path.join(packageDir, "package.json"), "utf8"));
  if (manifest.version !== version) {
    throw new Error(`${manifest.name}: version mismatch`);
  }
  for (const name of Object.keys(manifest.scripts || {})) {
    if (forbiddenScripts.has(name)) {
      throw new Error(`${manifest.name}: forbidden lifecycle script ${name}`);
    }
  }
  if (manifest.repository?.url !== "git+https://github.com/nxxxsooo/jumpotp.git") {
    throw new Error(`${manifest.name}: repository URL mismatch`);
  }
  if (manifest.name === "jumpotp") {
    await access(path.join(packageDir, "bin/jumpotp.js"));
    for (const dependencyVersion of Object.values(manifest.optionalDependencies || {})) {
      if (dependencyVersion !== version) throw new Error("root optional dependency version mismatch");
    }
  } else {
    if (!Array.isArray(manifest.os) || manifest.os.length !== 1 ||
        !Array.isArray(manifest.cpu) || manifest.cpu.length !== 1) {
      throw new Error(`${manifest.name}: invalid os/cpu metadata`);
    }
    const binary = path.join(packageDir, "bin/jumpotp");
    const info = await stat(binary);
    if ((info.mode & 0o111) === 0) throw new Error(`${manifest.name}: binary is not executable`);
    if (info.size === 0 || info.size > 50 * 1024 * 1024) throw new Error(`${manifest.name}: binary size is invalid`);
  }
}

console.log(`Audited ${packages.length} JumpOTP packages at ${version}.`);
