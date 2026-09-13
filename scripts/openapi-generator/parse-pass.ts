import { Node, Project, type SourceFile, type Type, type TypeAliasDeclaration } from "ts-morph";
import * as path from "path";
import type { Endpoint, Property } from "./types.js";
import { typeToProperties } from "./extract-params.js";

/**
 * Pass declares its API as two conditional types rather than one function per
 * endpoint: `ApiRequestBody<Path, Method>` and `ApiResponse<Path, Method>`, each
 * a chain of `Path extends \`pass/v1/…\` ? Method extends \`post\` ? Type : never : …`.
 *
 * Walking those chains gives every path, every method on it, and the named type
 * on either side - which is more than the rest of the API says, since it types
 * its responses too.
 */

const PASS_TYPES = "packages/pass/types/api/pass.ts";

type Declared = Map<string, Map<string, string>>;

export function parsePass(repoDir: string): Endpoint[] {
  const file = path.join(repoDir, PASS_TYPES);
  const project = new Project({
    tsConfigFilePath: path.join(repoDir, "tsconfig.base.json"),
    skipAddingFilesFromTsConfig: true,
  });
  const sf = project.addSourceFileAtPath(file);

  const bodies = declaredTypes(sf, "ApiRequestBody");
  const responses = declaredTypes(sf, "ApiResponse");

  const endpoints: Endpoint[] = [];
  for (const url of [...responses.keys()].sort()) {
    for (const [method, outputType] of responses.get(url)!) {
      const inputType = bodies.get(url)?.get(method) ?? "";
      endpoints.push(endpoint(sf, url, method, inputType, outputType));
    }
  }
  return endpoints;
}

/**
 * Walk one conditional-type chain into `path -> method -> type name`.
 */
function declaredTypes(sf: SourceFile, name: string): Declared {
  const alias = sf.getTypeAlias(name);
  if (!alias) return new Map();

  const out: Declared = new Map();
  let node: Node | undefined = alias.getTypeNode();

  while (node && Node.isConditionalTypeNode(node)) {
    const template = node.getExtendsType().getText();
    const url = urlOf(template);
    if (url) {
      const methods = out.get(url) ?? new Map<string, string>();
      collectMethods(node.getTrueType(), methods);
      if (methods.size > 0) out.set(url, methods);
    }
    node = node.getFalseType();
  }
  return out;
}

/**
 * The inner chain is one `Method extends \`post\` ? Type : …` per method.
 */
function collectMethods(node: Node, into: Map<string, string>): void {
  let current: Node | undefined = node;
  while (current && Node.isConditionalTypeNode(current)) {
    const method = literal(current.getExtendsType().getText());
    const type = current.getTrueType().getText();
    if (method && type !== "never") into.set(method, type);
    current = current.getFalseType();
  }
}

/**
 * `\`pass/v1/share/${string}/item\`` becomes `/pass/v1/share/{shareId}/item`.
 *
 * A placeholder is named after the segment in front of it, which is what the
 * path is about: the one after `share` is a share, the one after `item` an item.
 */
function urlOf(template: string): string {
  const raw = literal(template);
  if (!raw?.startsWith("pass/")) return "";

  const segments = raw.split("/");
  const out: string[] = [];
  for (const [i, segment] of segments.entries()) {
    if (segment !== "${string}") {
      out.push(segment);
      continue;
    }
    const previous = segments[i - 1] ?? "id";
    out.push(`{${singular(previous)}Id}`);
  }
  return "/" + out.join("/");
}

/** Strip the backticks or quotes a template literal type is written in. */
function literal(text: string): string {
  return text.replace(/^[`'"]|[`'"]$/g, "");
}

function singular(word: string): string {
  const clean = word.replace(/[^a-zA-Z]/g, "");
  if (clean.endsWith("ies")) return clean.slice(0, -3) + "y";
  if (clean.endsWith("s")) return clean.slice(0, -1);
  return clean || "id";
}

function endpoint(
  sf: SourceFile,
  url: string,
  method: string,
  inputType: string,
  outputType: string
): Endpoint {
  const pathParams = [...url.matchAll(/\{([^}]+)\}/g)].map((m) => m[1]);
  const bodyParams = propertiesOf(sf, inputType);

  return {
    name: operationId(method, url),
    method,
    url,
    tag: "pass",
    description: "",
    deprecated: false,
    isPublic: false,
    pathParams,
    queryParams: [],
    bodyParams,
    hasBody: inputType !== "" && inputType !== "any",
    inputType: "json",
    outputType: outputType === "string" ? "text" : "json",
    responseParams: propertiesOf(sf, outputType),
    timeout: "",
    keepalive: false,
    silencedErrors: [],
  };
}

/** The properties of a named type declared in the same file. */
function propertiesOf(sf: SourceFile, name: string): Property[] {
  if (!name || name === "any" || name === "never" || name === "string") return [];
  const alias: TypeAliasDeclaration | undefined = sf.getTypeAlias(name);
  const type: Type | undefined = alias?.getType() ?? inlineType(sf, name);
  if (!type) return [];
  return typeToProperties(type);
}

/**
 * A response written out where it is used rather than named, which is what the
 * one-field acknowledgements are.
 */
function inlineType(sf: SourceFile, name: string): Type | undefined {
  if (!name.startsWith("{")) return undefined;
  const alias = sf.addTypeAlias({ name: "__Inline", type: name });
  const type = alias.getType();
  alias.remove();
  return type;
}

/** postPassShareItemImportBatch, from the method and the path. */
function operationId(method: string, url: string): string {
  const words = url
    .split("/")
    .filter((segment) => segment && !segment.startsWith("{") && !/^v\d+$/.test(segment))
    .map((segment) =>
      segment
        .split(/[_-]/)
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join("")
    );
  return method + words.join("");
}
