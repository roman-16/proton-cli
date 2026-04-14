#!/usr/bin/env node

/**
 * Generates openapi.yaml from Proton WebClients TypeScript source files.
 *
 * 1. Shallow-clones ProtonMail/WebClients into /tmp
 * 2. Uses ts-morph for full AST parsing with type resolution
 * 3. Outputs openapi.yaml to stdout
 * 4. Cleans up the clone
 *
 * Usage: npm run generate-openapi
 */

import { ensureRepo } from "./clone.js";
import { parseAll } from "./parse.js";
import { generateOpenAPI } from "./emit-yaml.js";

const CLONE_DIR = "/tmp/proton-webclient-openapi";

async function main() {
  process.stderr.write("Ensuring ProtonMail/WebClients is up to date...\n");
  ensureRepo(CLONE_DIR);
  process.stderr.write("Ready.\n\n");

  process.stderr.write("Parsing API endpoints with ts-morph...\n");
  const { endpoints, enums } = parseAll(CLONE_DIR);
  process.stderr.write(`Found ${endpoints.length} endpoints, ${enums.size} enums\n\n`);

  process.stdout.write(generateOpenAPI(endpoints, enums));
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
