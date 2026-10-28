#!/usr/bin/env node
// Env:
//   VERSION  release tag/version, a leading "v" is stripped   (required)
//   BIN_DIR  directory holding the release binaries           (default: npm-bin)
//   DRY_RUN  "1" to run `npm publish --dry-run` (skips provenance)
import { execFileSync } from "node:child_process";
import { chmodSync, cpSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const SCOPE = "roman-16";
const NAME = "proton-cli";
const BIN = "proton";
const REPO = "https://github.com/roman-16/proton-cli";
const SITE = "https://proton-cli.lerchster.dev";
const DESCRIPTION =
  "Your CLI for Proton Mail, Drive, Calendar, Pass, Meet and Contacts. One binary, end-to-end encrypted.";
const KEYWORDS = [
  "cli",
  "command-line",
  "contacts",
  "end-to-end-encryption",
  "email",
  "proton",
  "proton-calendar",
  "proton-drive",
  "proton-mail",
  "proton-pass",
  "protonmail",
  "terminal",
];

const RAW = `${REPO.replace("github.com", "raw.githubusercontent.com")}/main`;
const BLOB = `${REPO}/blob/main`;

// npmjs.com does not resolve a README's relative links, so the published copy
// gets absolute ones: assets through raw.githubusercontent, the rest through
// the repository's file view.
const readmeForNpm = () =>
  readFileSync("README.md", "utf8")
    .replace(/(src|srcset)="(?!https?:)([^"]+)"/g, (_, attr, path) => `${attr}="${RAW}/${path}"`)
    .replace(/\]\((?!https?:|#|mailto:)([^)]+)\)/g, (_, path) => `](${BLOB}/${path})`);

const version = (process.env.VERSION || "").replace(/^v/, "");
if (!/^\d+\.\d+\.\d+/.test(version)) {
  console.error(`publish-npm: invalid VERSION ${JSON.stringify(process.env.VERSION)}`);
  process.exit(1);
}
const binDir = process.env.BIN_DIR || "npm-bin";
const dryRun = process.env.DRY_RUN === "1";
const prerelease = version.includes("-");

const PLATFORMS = {
  "linux-x64": { asset: `${NAME}_linux_amd64`, bin: BIN },
  "linux-arm64": { asset: `${NAME}_linux_arm64`, bin: BIN },
  "darwin-x64": { asset: `${NAME}_darwin_amd64`, bin: BIN },
  "darwin-arm64": { asset: `${NAME}_darwin_arm64`, bin: BIN },
  "win32-x64": { asset: `${NAME}_windows_amd64.exe`, bin: `${BIN}.exe` },
  "win32-arm64": { asset: `${NAME}_windows_arm64.exe`, bin: `${BIN}.exe` },
};

const shim = `#!/usr/bin/env node
"use strict";
const { spawnSync } = require("node:child_process");
const PKGS = {
  "linux-x64": "@${SCOPE}/${NAME}-linux-x64",
  "linux-arm64": "@${SCOPE}/${NAME}-linux-arm64",
  "darwin-x64": "@${SCOPE}/${NAME}-darwin-x64",
  "darwin-arm64": "@${SCOPE}/${NAME}-darwin-arm64",
  "win32-x64": "@${SCOPE}/${NAME}-win32-x64",
  "win32-arm64": "@${SCOPE}/${NAME}-win32-arm64",
};
const key = process.platform + "-" + process.arch;
const pkg = PKGS[key];
if (!pkg) {
  console.error("${BIN}: no prebuilt binary for " + key + ". See ${REPO}");
  process.exit(1);
}
const exe = process.platform === "win32" ? "${BIN}.exe" : "${BIN}";
let binPath;
try {
  binPath = require.resolve(pkg + "/bin/" + exe);
} catch {
  console.error("${BIN}: platform package " + pkg + " is not installed. See ${REPO}");
  process.exit(1);
}
const res = spawnSync(binPath, process.argv.slice(2), { stdio: "inherit" });
if (res.error) throw res.error;
process.exit(res.status === null ? 1 : res.status);
`;

const work = join(tmpdir(), `npm-${NAME}-${version}`);
rmSync(work, { recursive: true, force: true });

const writeJSON = (path, obj) => writeFileSync(path, JSON.stringify(obj, null, 2) + "\n");
const publish = (dir) => {
  const args = ["publish", "--access", "public"];
  if (dryRun) args.push("--dry-run");
  else args.push("--provenance");
  if (prerelease) args.push("--tag", "next");
  execFileSync("npm", args, { cwd: dir, stdio: "inherit" });
};

const npmReadme = readmeForNpm();

const optionalDependencies = {};
for (const [key, { asset, bin }] of Object.entries(PLATFORMS)) {
  const [os, cpu] = key.split("-");
  const pkgName = `@${SCOPE}/${NAME}-${key}`;
  const dir = join(work, key);
  mkdirSync(join(dir, "bin"), { recursive: true });
  cpSync(join(binDir, asset), join(dir, "bin", bin));
  chmodSync(join(dir, "bin", bin), 0o755);
  writeFileSync(join(dir, "README.md"), npmReadme);
  cpSync("LICENSE", join(dir, "LICENSE"));
  writeJSON(join(dir, "package.json"), {
    name: pkgName,
    version,
    description: `proton-cli native binary for ${key}`,
    license: "MIT",
    homepage: REPO,
    repository: { type: "git", url: `git+${REPO}.git` },
    os: [os],
    cpu: [cpu],
    preferUnplugged: true,
    files: ["bin"],
  });
  optionalDependencies[pkgName] = version;
  console.log(`packaged ${pkgName}@${version}`);
}

const rootDir = join(work, "root");
mkdirSync(join(rootDir, "bin"), { recursive: true });
writeFileSync(join(rootDir, "bin", `${BIN}.js`), shim);
chmodSync(join(rootDir, "bin", `${BIN}.js`), 0o755);
writeFileSync(join(rootDir, "README.md"), npmReadme);
cpSync("LICENSE", join(rootDir, "LICENSE"));
writeJSON(join(rootDir, "package.json"), {
  name: `@${SCOPE}/${NAME}`,
  version,
  description: DESCRIPTION,
  keywords: KEYWORDS,
  license: "MIT",
  homepage: SITE,
  repository: { type: "git", url: `git+${REPO}.git` },
  bin: { [BIN]: `bin/${BIN}.js`, [NAME]: `bin/${BIN}.js` },
  files: ["bin"],
  optionalDependencies,
});
console.log(`packaged @${SCOPE}/${NAME}@${version}`);

for (const key of Object.keys(PLATFORMS)) publish(join(work, key));
publish(rootDir);
console.log(dryRun ? "npm dry-run complete" : `published @${SCOPE}/${NAME}@${version} + ${Object.keys(PLATFORMS).length} platform packages`);
