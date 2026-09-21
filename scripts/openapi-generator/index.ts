#!/usr/bin/env node

/**
 * Generates an OpenAPI 3.1 spec on stdout from a Proton WebClients checkout and
 * a Proton Drive SDK checkout, parsed with ts-morph for full type resolution.
 *
 * Usage: bun openapi-generator/index.ts WEBCLIENTS_DIR DRIVE_SDK_DIR
 */

import { parseAll } from "./parse.js";
import { generateOpenAPI } from "./emit-yaml.js";

const webClientsDir = process.argv[2];
const driveSdkDir = process.argv[3];
if (!webClientsDir || !driveSdkDir) {
  process.stderr.write("usage: bun openapi-generator/index.ts WEBCLIENTS_DIR DRIVE_SDK_DIR\n");
  process.exit(1);
}

process.stderr.write("Parsing API endpoints with ts-morph...\n");
const { routes, enums } = parseAll(webClientsDir, driveSdkDir);
const operations = routes.reduce((total, route) => total + route.operations.size, 0);
process.stderr.write(
  `Found ${operations} endpoints across ${routes.length} paths, ${enums.size} enums\n\n`
);

process.stdout.write(generateOpenAPI(routes, enums));
