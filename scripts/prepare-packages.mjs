#!/usr/bin/env node

import { chmod, copyFile, mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const version = (await readFile(path.join(root, "VERSION"), "utf8")).trim();
if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) {
  throw new Error(`VERSION is not publishable semver: ${version}`);
}

const platforms = [
  ["darwin-arm64", "jumpotp-darwin-arm64"],
  ["darwin-x64", "jumpotp-darwin-x64"],
  ["linux-arm64", "jumpotp-linux-arm64"],
  ["linux-x64", "jumpotp-linux-x64"]
];

for (const [target] of platforms) {
  const packageDir = path.join(root, "packages/npm/platform", target);
  const binDir = path.join(packageDir, "bin");
  await mkdir(binDir, { recursive: true });
  const destination = path.join(binDir, "jumpotp");
  await copyFile(path.join(root, "dist", `jumpotp-${target}`), destination);
  await chmod(destination, 0o755);
  await updateManifest(path.join(packageDir, "package.json"), (manifest) => {
    manifest.version = version;
  });
}

await updateManifest(path.join(root, "packages/npm/jumpotp/package.json"), (manifest) => {
  manifest.version = version;
  for (const [, packageName] of platforms) {
    manifest.optionalDependencies[packageName] = version;
  }
});

await updateManifest(path.join(root, "package.json"), (manifest) => {
  manifest.version = version;
});

async function updateManifest(file, update) {
  const manifest = JSON.parse(await readFile(file, "utf8"));
  update(manifest);
  await writeFile(file, JSON.stringify(manifest, null, 2) + "\n");
}
