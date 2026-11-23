import { Project } from "ts-morph";
import * as path from "path";
import { readdirSync, readFileSync, statSync } from "fs";
import type { Endpoint, EnumInfo } from "./types.js";
import { collectEnums, ENUM_MAP } from "./registry.js";
import { extractRequests } from "./extract-endpoint.js";
import { merge, type Route } from "./merge.js";
import { parseDriveSdk } from "./parse-drive-sdk.js";
import { parsePass } from "./parse-pass.js";

const ROOTS = ["applications", "packages"];

// A test declares the same literals as the request it checks, and a mock
// answers requests rather than sending them.
const TEST_FILE = /\.(spec|stories|test)\.tsx?$/;
const TEST_DIRS = new Set(["__mocks__", "__tests__", "mock", "mocks", "test", "testing", "tests"]);

// The web clients keep the enums their requests take in the shared package.
const ENUM_PACKAGE = "packages/shared/lib";

export function parseAll(
  webClientsDir: string,
  driveSdkDir: string
): { routes: Route[]; enums: Map<string, EnumInfo> } {
  const project = new Project({
    tsConfigFilePath: path.join(webClientsDir, "tsconfig.base.json"),
    skipAddingFilesFromTsConfig: true,
    compilerOptions: { paths: { "@proton/*": [path.join(webClientsDir, "packages/*")] } },
  });

  const requestFiles = ROOTS.flatMap((root) => sourceFiles(path.join(webClientsDir, root)))
    .filter((file) => mayDeclareRequest(file))
    .sort();
  for (const file of [...typeScriptFiles(path.join(webClientsDir, ENUM_PACKAGE)), ...requestFiles]) {
    if (!project.getSourceFile(file)) project.addSourceFileAtPath(file);
  }

  for (const sf of project.getSourceFiles()) {
    collectEnums(sf);
  }

  const requests = requestFiles.map((file) => extractRequests(project.getSourceFileOrThrow(file)));
  const declared: Endpoint[] = requests.flatMap((r) => r.declared);
  const sent: Endpoint[] = requests.flatMap((r) => r.sent);

  // Drive's own spec says more about Drive than the web client does, and Pass
  // declares its API as types in a package of its own, which say more than
  // the requests its client writes.
  const routes = merge([parseDriveSdk(driveSdkDir), parsePass(webClientsDir), declared, sent]);

  return { routes, enums: ENUM_MAP };
}

function sourceFiles(dir: string): string[] {
  const files: string[] = [];
  for (const entry of readdirSync(dir)) {
    if (entry === "node_modules" || entry.startsWith(".") || TEST_DIRS.has(entry)) continue;
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) {
      files.push(...sourceFiles(full));
    } else if (/\.tsx?$/.test(entry) && !entry.endsWith(".d.ts") && !TEST_FILE.test(entry)) {
      files.push(full);
    }
  }
  return files;
}

function typeScriptFiles(dir: string): string[] {
  return sourceFiles(dir).filter((file) => file.endsWith(".ts"));
}

function mayDeclareRequest(file: string): boolean {
  const text = readFileSync(file, "utf8");
  return /\burl\b/.test(text) && /\bmethod\b/.test(text);
}
