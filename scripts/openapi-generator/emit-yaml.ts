import type { Endpoint, EnumInfo } from "./types.js";
import type { Route } from "./merge.js";

export function generateOpenAPI(routes: Route[], enums: Map<string, EnumInfo>): string {
  const operations = routes.flatMap((route) => [...route.operations.values()]);
  const tags = [...new Set(operations.map((e) => e.tag))].sort();
  const lines: string[] = [];

  emitHeader(lines);
  emitEnumComments(lines, enums);
  emitTags(lines, tags);
  emitPaths(lines, routes);

  return lines.join("\n") + "\n";
}

function emitHeader(lines: string[]): void {
  lines.push("openapi: 3.1.0");
  lines.push("info:");
  lines.push("  title: Proton API");
  lines.push('  version: "1.0"');
  lines.push("  description: |");
  lines.push("    Auto-generated from Proton's own TypeScript source files.");
  lines.push("    Sources: https://github.com/ProtonMail/WebClients/tree/main/packages/shared/lib/api");
  lines.push("             https://github.com/ProtonMail/WebClients/blob/main/packages/pass/types/api/pass.ts");
  lines.push("             https://github.com/ProtonDriveApps/sdk/blob/main/client/js/src/internal/apiService/driveTypes.ts");
  lines.push("");
  lines.push("servers:");
  lines.push("  - url: https://mail.proton.me/api");
  lines.push("    description: Proton production API");
  lines.push("");
  lines.push("security:");
  lines.push("  - protonAuth: []");
  lines.push("");
  lines.push("components:");
  lines.push("  securitySchemes:");
  lines.push("    protonAuth:");
  lines.push("      type: http");
  lines.push("      scheme: bearer");
  lines.push("      description: |");
  lines.push("        Requires headers: Authorization (Bearer token), x-pm-uid (session UID), x-pm-appversion.");
  lines.push("  schemas:");
  lines.push("    ApiResponse:");
  lines.push("      type: object");
  lines.push("      description: Standard Proton API JSON response envelope.");
  lines.push("      properties:");
  lines.push("        Code:");
  lines.push("          type: integer");
  lines.push("          description: Proton response code (1000/1001 indicate success).");
  lines.push("");
}

function emitEnumComments(lines: string[], enums: Map<string, EnumInfo>): void {
  if (enums.size === 0) return;
  lines.push("  # Enum reference:");
  for (const [name, def] of [...enums.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
    if (def.values.length === 0) continue;
    const desc = def.values.map((v) => `${v.value}=${v.key}`).join(", ");
    lines.push(`  # ${name}: ${desc}`);
  }
  lines.push("");
}

function emitTags(lines: string[], tags: string[]): void {
  lines.push("tags:");
  for (const tag of tags) lines.push(`  - name: ${tag}`);
  lines.push("");
}

function emitPaths(lines: string[], routes: Route[]): void {
  lines.push("paths:");

  for (const route of routes) {
    lines.push(`  ${route.url}:`);
    for (const [method, ep] of route.operations) {
      emitOperation(lines, method, ep);
    }
  }
}

function emitOperation(lines: string[], method: string, ep: Endpoint): void {
  lines.push(`    ${method}:`);
  lines.push(`      tags: [${ep.tag}]`);
  lines.push(`      summary: ${esc(ep.summary || camelToTitle(ep.name))}`);
  lines.push(`      operationId: ${ep.name}`);

  if (ep.description) lines.push(`      description: ${esc(ep.description)}`);
  if (ep.deprecated) lines.push("      deprecated: true");
  if (ep.isPublic) lines.push("      security: []");

  // Parameters
  if (ep.pathParams.length > 0 || ep.queryParams.length > 0) {
    lines.push("      parameters:");
    for (const p of ep.pathParams) {
      lines.push(`        - name: ${p}`);
      lines.push("          in: path");
      lines.push("          required: true");
      lines.push("          schema:");
      lines.push("            type: string");
    }
    for (const f of ep.queryParams) {
      lines.push(`        - name: ${f.name}`);
      lines.push("          in: query");
      lines.push("          schema:");
      lines.push(`            type: ${f.type}`);
      if (f.description) lines.push(`          description: ${esc(f.description)}`);
    }
  }

  // Request body
  if (ep.hasBody && ["post", "put", "patch", "delete"].includes(method)) {
    lines.push("      requestBody:");
    lines.push("        content:");

    const contentType = inputContentType(ep.inputType);
    lines.push(`          ${contentType}:`);
    lines.push("            schema:");

    if (ep.bodyParams.length > 0) {
      lines.push("              type: object");
      const required = ep.bodyParams.filter((f) => !f.optional).map((f) => f.name);
      if (required.length > 0) {
        lines.push("              required:");
        for (const name of required) lines.push(`                - ${name}`);
      }
      lines.push("              properties:");
      for (const f of ep.bodyParams) {
        lines.push(`                ${f.name}:`);
        lines.push(`                  type: ${f.type}`);
        if (f.type === "array") lines.push("                  items: {}");
        if (f.description) lines.push(`                  description: ${esc(f.description)}`);
      }
    } else {
      lines.push("              type: object");
    }
  }

  // Response
  lines.push("      responses:");
  lines.push('        "200":');
  lines.push("          description: Success");
  lines.push("          content:");

  const responseType = outputContentType(ep.outputType);
  lines.push(`            ${responseType}:`);
  lines.push("              schema:");

  if (responseType === "application/json" && ep.responseParams?.length) {
    // Part of the API types what comes back as well as what goes out.
    lines.push("                allOf:");
    lines.push("                  - $ref: '#/components/schemas/ApiResponse'");
    lines.push("                  - type: object");
    lines.push("                    properties:");
    for (const f of ep.responseParams) {
      lines.push(`                      ${f.name}:`);
      lines.push(`                        type: ${f.type}`);
      if (f.type === "array") lines.push("                        items: {}");
      if (f.description) lines.push(`                        description: ${esc(f.description)}`);
    }
  } else if (responseType === "application/json") {
    lines.push("                $ref: '#/components/schemas/ApiResponse'");
  } else if (responseType === "text/plain") {
    lines.push("                type: string");
  } else {
    lines.push("                type: string");
    lines.push("                format: binary");
  }

  // Extensions
  if (ep.timeout) lines.push(`      x-timeout: ${ep.timeout}`);
  if (ep.keepalive) lines.push("      x-keepalive: true");
  if (ep.silencedErrors.length > 0) {
    lines.push(`      x-expected-errors: [${ep.silencedErrors.join(", ")}]`);
  }
}

function inputContentType(input: string): string {
  switch (input) {
    case "form": return "multipart/form-data";
    case "binary": return "application/octet-stream";
    default: return "application/json";
  }
}

function outputContentType(output: string): string {
  switch (output) {
    case "stream":
    case "arrayBuffer": return "application/octet-stream";
    case "text": return "text/plain";
    case "raw": return "application/json";
    default: return "application/json";
  }
}

function camelToTitle(name: string): string {
  return name
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .replace(/^./, (s) => s.toUpperCase())
    .trim();
}

// A plain scalar may not carry these, and may not open with one of those a
// block or a tag starts with.
const UNQUOTABLE = /[:#{}'"]/;
const INDICATOR = /^[-?,[\]&*!|>%@`]/;

function esc(s: string): string {
  if (!s) return s;
  const flat = s.replace(/\s+/g, " ").trim();
  if (UNQUOTABLE.test(flat) || INDICATOR.test(flat)) {
    return `"${flat.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
  }
  return flat;
}
