import { Node, Project, type JSDoc, type Type } from "ts-morph";
import * as path from "path";
import type { Endpoint } from "./types.js";
import { typeToProperties } from "./extract-params.js";

/**
 * Drive's web app calls `@protontech/drive-sdk` rather than the shared API
 * package, so Drive's endpoints are declared in the SDK's repository: Proton's
 * own OpenAPI, written out by `openapi-typescript` as a `paths` interface that
 * maps each URL to one `operations["…"]` reference per method, and an
 * `operations` interface that names the parameters, the body and the answer.
 */

const DRIVE_TYPES = "client/js/src/internal/apiService/driveTypes.ts";
const METHODS = ["delete", "get", "patch", "post", "put"];
const SUCCESS_CODES = ["200", "202"];
const MEDIA_TYPES: Record<string, string> = {
  "application/json": "json",
  "multipart/form-data": "form",
};

export function parseDriveSdk(repoDir: string): Endpoint[] {
  const project = new Project({ compilerOptions: { strict: true } });
  const sf = project.addSourceFileAtPath(path.join(repoDir, DRIVE_TYPES));

  const endpoints: Endpoint[] = [];
  for (const route of sf.getInterfaceOrThrow("paths").getProperties()) {
    const url = unquote(route.getName());
    const routeType = route.getType();

    for (const method of METHODS) {
      const declared = routeType.getProperty(method)?.getValueDeclaration();
      if (!declared || !Node.isPropertySignature(declared)) continue;

      const key = operationKey(declared.getTypeNode()?.getText() ?? "");
      if (!key) continue;

      endpoints.push(
        endpoint(url, method, key, declared.getType(), declared, declared.getJsDocs()[0])
      );
    }
  }
  return endpoints;
}

function endpoint(
  url: string,
  method: string,
  key: string,
  operation: Type,
  at: Node,
  jsDoc: JSDoc | undefined
): Endpoint {
  const query = member(member(operation, "parameters", at), "query", at);
  const body = content(member(operation, "requestBody", at), at);
  const answer = content(successResponse(member(operation, "responses", at), at), at);

  return {
    name: operationId(key),
    method,
    url,
    tag: "Drive",
    summary: flatten(jsDoc?.getDescription() ?? ""),
    description: describe(jsDoc, member(operation, "responses", at), at),
    deprecated: false,
    isPublic: false,
    pathParams: [...url.matchAll(/\{([^}]+)\}/g)].map((m) => m[1]),
    queryParams: query ? typeToProperties(query) : [],
    bodyParams: body ? typeToProperties(body.type) : [],
    responseParams: answer ? typeToProperties(answer.type) : [],
    hasBody: body !== undefined,
    inputType: body?.input ?? "",
    outputType: "json",
    timeout: "",
    keepalive: false,
    silencedErrors: [],
  };
}

/**
 * The description says what the summary does not: the operation's own prose
 * where it has any, otherwise the meaning Proton gives the codes it rejects a
 * call with.
 */
function describe(jsDoc: JSDoc | undefined, responses: Type | undefined, at: Node): string {
  const tag = jsDoc?.getTags().find((t) => t.getTagName() === "description");
  const prose = flatten(tag?.getCommentText() ?? "");
  if (prose) return prose;

  const rejected = content(member(responses, "422", at), at);
  if (!rejected) return "";
  return typeToProperties(rejected.type).find((p) => p.name === "Code")?.description ?? "";
}

function successResponse(responses: Type | undefined, at: Node): Type | undefined {
  for (const code of SUCCESS_CODES) {
    const response = member(responses, code, at);
    if (response) return response;
  }
  return undefined;
}

function content(
  carrier: Type | undefined,
  at: Node
): { type: Type; input: string } | undefined {
  const carried = member(carrier, "content", at);
  if (!carried) return undefined;

  for (const [mediaType, input] of Object.entries(MEDIA_TYPES)) {
    const type = member(carried, mediaType, at);
    if (type) return { type, input };
  }
  return undefined;
}

function member(type: Type | undefined, name: string, at: Node): Type | undefined {
  const symbol = type?.getProperty(name);
  if (!symbol) return undefined;
  const resolved = symbol.getTypeAtLocation(at).getNonNullableType();
  return resolved.isNever() ? undefined : resolved;
}

/** `operations["get_drive-photos-volumes-{volumeID}-albums"]` names the operation. */
function operationKey(typeText: string): string {
  return /^operations\["(.+)"\]$/.exec(typeText)?.[1] ?? "";
}

/**
 * getDrivePhotosVolumesByVolumeIDAlbums, from that name. A placeholder is part
 * of it: dropping them would name the listing and the one-item read alike.
 */
function operationId(key: string): string {
  const [method, route = ""] = splitOnce(key, "_");
  const words = route
    .split("-")
    .filter((word) => word)
    .map((word) =>
      word.startsWith("{") ? "By" + capitalize(unbrace(word)) : capitalize(word)
    );
  return method + words.join("");
}

function capitalize(word: string): string {
  return word.charAt(0).toUpperCase() + word.slice(1);
}

function unbrace(word: string): string {
  return word.replace(/[{}]/g, "");
}

function splitOnce(text: string, separator: string): [string, string?] {
  const at = text.indexOf(separator);
  return at === -1 ? [text] : [text.slice(0, at), text.slice(at + separator.length)];
}

function unquote(text: string): string {
  return text.replace(/^["']|["']$/g, "");
}

function flatten(text: string): string {
  return text.replace(/\s+/g, " ").trim();
}
