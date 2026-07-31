#!/usr/bin/env node

const packages = [
  "jumpotp",
  "jumpotp-darwin-arm64",
  "jumpotp-darwin-x64",
  "jumpotp-linux-arm64",
  "jumpotp-linux-x64"
];

for (const packageName of packages) {
  const response = await fetch(`https://registry.npmjs.org/${packageName}`);
  if (response.status !== 404) {
    throw new Error(`${packageName} is no longer available (registry status ${response.status})`);
  }
  console.log(`${packageName}: available`);
}
