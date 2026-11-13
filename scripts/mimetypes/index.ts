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
import { writeFileSync } from "fs";
import * as path from "path";
import mime from "mime-types";
import pkg from "mime-types/package.json";

import { resolvedVersion } from "../webclients";

function main() {
  const webClientsDir = process.argv[2];
  if (!webClientsDir) {
    process.stderr.write("usage: bun mimetypes/index.ts WEBCLIENTS_DIR\n");
    process.exit(1);
  }

  const pinned = resolvedVersion(webClientsDir, "packages/drive", "mime-types");
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
