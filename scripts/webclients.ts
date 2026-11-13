import { readFileSync } from "fs";
import * as path from "path";

type Resolution = { version: string };
type Importer = { dependencies?: Record<string, Resolution>; devDependencies?: Record<string, Resolution> };
type Lockfile = { importers?: Record<string, Importer> };

function fail(message: string): never {
  process.stderr.write(message + "\n");
  process.exit(1);
}

export function resolvedVersion(webClientsDir: string, importer: string, dependency: string): string {
  const documents: Lockfile[] = [Bun.YAML.parse(readFileSync(path.join(webClientsDir, "pnpm-lock.yaml"), "utf8"))].flat();
  const declared = documents.find((document) => document?.importers?.[importer])?.importers?.[importer];
  if (!declared) {
    fail(`WebClients' pnpm-lock.yaml has no ${importer}`);
  }
  const resolution = declared.dependencies?.[dependency] ?? declared.devDependencies?.[dependency];
  if (!resolution) {
    fail(`WebClients' ${importer} names no ${dependency}`);
  }
  return resolution.version.split("(")[0];
}
