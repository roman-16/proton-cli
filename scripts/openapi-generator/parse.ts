import { Project } from "ts-morph";
import * as path from "path";
import { readdirSync, statSync } from "fs";
import type { Endpoint, EnumInfo } from "./types.js";
import { collectConstants, collectEnums, ENUM_MAP } from "./registry.js";
import { extractFromArrow, extractFromFunction } from "./extract-endpoint.js";
import { merge, type Route } from "./merge.js";
import { parseDriveSdk } from "./parse-drive-sdk.js";
import { parsePass } from "./parse-pass.js";

const SKIP_FILES = new Set([
  "createApi.ts",
  "apiEnvironmentConfig.ts",
  "apiRateLimiter.ts",
  "interface.ts",
  "docs.ts",
]);

export function parseAll(
  webClientsDir: string,
  driveSdkDir: string
): { routes: Route[]; enums: Map<string, EnumInfo> } {
  const sharedLib = path.join(webClientsDir, "packages/shared/lib");

  const project = new Project({
    tsConfigFilePath: path.join(webClientsDir, "tsconfig.base.json"),
    skipAddingFilesFromTsConfig: true,
  });

  addFilesRecursive(project, sharedLib);

  // Build the constant/enum registry from all source files
  for (const sf of project.getSourceFiles()) {
    collectConstants(sf);
    collectEnums(sf);
  }

  // Parse endpoints from api/ files only
  const apiFiles = project
    .getSourceFiles()
    .filter((sf) => sf.getFilePath().includes("/api/") && !sf.getFilePath().includes("/helpers/"))
    .filter((sf) => !SKIP_FILES.has(path.basename(sf.getFilePath())));

  // A request is declared by the object a function returns, whether or not the
  // module lets anyone else call it: upstream keeps helpers behind exported
  // wrappers, and the endpoint is the same endpoint either way.
  const endpoints: Endpoint[] = [];
  for (const sf of apiFiles) {
    for (const decl of sf.getVariableDeclarations()) {
      const init = decl.getInitializer();
      if (!init) continue;
      const ep = extractFromArrow(decl.getName(), init, decl);
      if (ep) endpoints.push(ep);
    }
    for (const decl of sf.getFunctions()) {
      const name = decl.getName();
      if (!name) continue;
      const ep = extractFromFunction(name, decl);
      if (ep) endpoints.push(ep);
    }
  }

  // Drive's own spec says more about Drive than the web client does, and Pass
  // declares its API as types rather than as functions, in a package of its own.
  const routes = merge([parseDriveSdk(driveSdkDir), endpoints, parsePass(webClientsDir)]);

  return { routes, enums: ENUM_MAP };
}

function addFilesRecursive(project: Project, dir: string): void {
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    const stat = statSync(full);
    if (stat.isDirectory()) {
      addFilesRecursive(project, full);
    } else if (entry.endsWith(".ts") && !entry.endsWith(".d.ts")) {
      project.addSourceFileAtPath(full);
    }
  }
}
