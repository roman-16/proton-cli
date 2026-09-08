#!/usr/bin/env node

/**
 * Generates an OpenAPI 3.1 spec on stdout from a Proton WebClients checkout,
 * parsed with ts-morph for full type resolution.
 *
 * Usage: bun openapi-generator/index.ts WEBCLIENTS_DIR
 */

import { parseAll } from "./parse.js";
import { generateOpenAPI } from "./emit-yaml.js";

const repoDir = process.argv[2];
if (!repoDir) {
  process.stderr.write("usage: bun openapi-generator/index.ts WEBCLIENTS_DIR\n");
  process.exit(1);
}

process.stderr.write("Parsing API endpoints with ts-morph...\n");
const { endpoints, enums } = parseAll(repoDir);
process.stderr.write(`Found ${endpoints.length} endpoints, ${enums.size} enums\n\n`);

process.stdout.write(generateOpenAPI(endpoints, enums));
