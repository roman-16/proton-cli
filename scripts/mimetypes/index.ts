#!/usr/bin/env bun
/**
 * Writes the table internal/mimetype types a file by its name with, from the
 * mime-types release Proton Drive's web client uploads with.
 *
 * The table is the library's own answer for every extension it knows, so a
 * file typed here is typed as the web client types it, on every machine.
 *
 * Usage: bun mimetypes/index.ts WEBCLIENTS_DIR
 */
import { readFileSync, writeFileSync } from "fs";
import * as path from "path";
import mime from "mime-types";
import pkg from "mime-types/package.json";

function webClientsVersion(dir: string): string {
  const manifest = JSON.parse(readFileSync(path.join(dir, "packages", "drive-store", "package.json"), "utf8"));
  const range = manifest.dependencies?.["mime-types"];
  if (!range) {
    process.stderr.write("WebClients' Drive names no mime-types dependency\n");
    process.exit(1);
  }
  const lock = readFileSync(path.join(dir, "yarn.lock"), "utf8");
  for (const entry of lock.split("\n\n")) {
    const [key, ...rest] = entry.trim().split("\n");
    if (!key?.split(", ").some((spec) => spec.replace(/[":]/g, "") === `mime-types@npm${range}`)) {
      continue;
    }
    const version = rest.map((line) => line.match(/^\s*version: (\S+)/)).find(Boolean);
    if (version) {
      return version[1];
    }
  }
  process.stderr.write(`WebClients' yarn.lock resolves no mime-types@${range}\n`);
  process.exit(1);
}

function main() {
  const webClientsDir = process.argv[2];
  if (!webClientsDir) {
    process.stderr.write("usage: bun mimetypes/index.ts WEBCLIENTS_DIR\n");
    process.exit(1);
  }

  const pinned = webClientsVersion(webClientsDir);
  if (pinned !== pkg.version) {
    process.stderr.write(`WebClients' Drive uploads with mime-types ${pinned}, scripts/package.json pins ${pkg.version}\n`);
    process.exit(1);
  }

  const table = Object.fromEntries(Object.entries(mime.types).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));
  const out = path.join(import.meta.dir, "..", "..", "internal", "mimetype", "extensions.json");
  writeFileSync(out, JSON.stringify(table, null, 2) + "\n");
  process.stderr.write(`mime-types ${pkg.version}: ${Object.keys(table).length} extensions\n`);
}

main();
