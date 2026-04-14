import type { Endpoint, Property, EnumInfo } from "./parse.js";

export function generateOpenAPI(
  endpoints: Endpoint[],
  enums: Map<string, EnumInfo>
): string {
  const tags = [...new Set(endpoints.map((e) => e.tag))].sort();
  const lines: string[] = [];

  emitHeader(lines);
  emitEnumComments(lines, enums);
  emitTags(lines, tags);
  emitPaths(lines, endpoints);

  return lines.join("\n") + "\n";
}

function emitHeader(lines: string[]): void {
  lines.push("openapi: 3.1.0");
  lines.push("info:");
  lines.push("  title: Proton API");
  lines.push('  version: "1.0"');
  lines.push("  description: |");
  lines.push("    Auto-generated from ProtonMail/WebClients TypeScript source files.");
  lines.push("    Source: https://github.com/ProtonMail/WebClients/tree/main/packages/shared/lib/api");
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

function emitPaths(lines: string[], endpoints: Endpoint[]): void {
  const byPath = new Map<string, Endpoint[]>();
  for (const ep of endpoints) {
    const existing = byPath.get(ep.url) || [];
    existing.push(ep);
    byPath.set(ep.url, existing);
  }

  lines.push("paths:");

  for (const urlPath of [...byPath.keys()].sort()) {
    const eps = byPath.get(urlPath)!;
    lines.push(`  ${urlPath}:`);

    const byMethod = new Map<string, Endpoint>();
    for (const ep of eps) {
      if (!byMethod.has(ep.method)) byMethod.set(ep.method, ep);
    }

    for (const [method, ep] of byMethod) {
      emitOperation(lines, method, ep);
    }
  }
}

function emitOperation(lines: string[], method: string, ep: Endpoint): void {
  lines.push(`    ${method}:`);
  lines.push(`      tags: [${ep.tag}]`);
  lines.push(`      summary: ${camelToTitle(ep.name)}`);
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
      lines.push("              properties:");
      for (const f of ep.bodyParams) {
        lines.push(`                ${f.name}:`);
        lines.push(`                  type: ${f.type}`);
        if (f.optional) lines.push("                  nullable: true");
        if (f.description) lines.push(`                  description: ${esc(f.description)}`);
      }
    } else {
      lines.push("              type: object");
    }
  }

  // Response
  lines.push("      responses:");
  lines.push('        "200":');
  lines.push(`          description: ${camelToTitle(ep.name)}`);
  lines.push("          content:");

  const responseType = outputContentType(ep.outputType);
  lines.push(`            ${responseType}:`);
  lines.push("              schema:");

  if (responseType === "application/json") {
    lines.push("                type: object");
    lines.push("                properties:");
    lines.push("                  Code:");
    lines.push("                    type: integer");
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
  return name.replace(/([A-Z])/g, " $1").replace(/^./, (s) => s.toUpperCase()).trim();
}

function esc(s: string): string {
  if (!s) return s;
  if (/[:#{}'"]/.test(s)) return `"${s.replace(/"/g, '\\"')}"`;
  return s;
}
