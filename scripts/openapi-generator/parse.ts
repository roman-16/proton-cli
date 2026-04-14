import { Project, Node } from "ts-morph";
import * as path from "path";
import { readdirSync, statSync } from "fs";
import type { Endpoint, EnumInfo } from "./types.js";
import { collectConstants, collectEnums, ENUM_MAP } from "./registry.js";
import { extractFromArrow, extractFromFunction } from "./extract-endpoint.js";

const SKIP_FILES = new Set([
  "createApi.ts",
  "apiEnvironmentConfig.ts",
  "apiRateLimiter.ts",
  "interface.ts",
  "docs.ts",
]);

export function parseAll(repoDir: string): { endpoints: Endpoint[]; enums: Map<string, EnumInfo> } {
  const sharedLib = path.join(repoDir, "packages/shared/lib");

  const project = new Project({
    tsConfigFilePath: path.join(repoDir, "tsconfig.base.json"),
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

  const endpoints: Endpoint[] = [];
  for (const sf of apiFiles) {
    for (const [name, decls] of sf.getExportedDeclarations()) {
      for (const decl of decls) {
        if (Node.isVariableDeclaration(decl)) {
          const init = decl.getInitializer();
          if (!init) continue;
          const ep = extractFromArrow(name, init, decl);
          if (ep) endpoints.push(ep);
        }
        if (Node.isFunctionDeclaration(decl)) {
          const ep = extractFromFunction(name, decl);
          if (ep) endpoints.push(ep);
        }
      }
    }
  }

  return { endpoints, enums: ENUM_MAP };
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
