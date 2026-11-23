import {
  Node,
  SyntaxKind,
  type ObjectLiteralExpression,
  type SourceFile,
  type Symbol as TsSymbol,
  type Type,
} from "ts-morph";
import type { Endpoint } from "./types.js";
import { typeToProperties } from "./extract-params.js";

const METHODS = new Set(["delete", "get", "head", "patch", "post", "put"]);

export interface Requests {
  declared: Endpoint[];
  sent: Endpoint[];
}

// A request is an object with a `url` and a `method`, wherever the web clients
// write it. One a function returns or a module keeps in a constant is declared
// for every caller; one handed straight to the API is sent by the code around
// it, which may name it after an event handler and send only part of its body.
export function extractRequests(sf: SourceFile): Requests {
  const requests: Requests = { declared: [], sent: [] };
  for (const obj of sf.getDescendantsOfKind(SyntaxKind.ObjectLiteralExpression)) {
    const endpoint = requestFrom(obj);
    if (endpoint) (isDeclaration(obj) ? requests.declared : requests.sent).push(endpoint);
  }
  return requests;
}

function isDeclaration(obj: Node): boolean {
  let node = obj;
  let parent = node.getParent();
  while (
    parent &&
    (Node.isParenthesizedExpression(parent) ||
      Node.isAsExpression(parent) ||
      Node.isSatisfiesExpression(parent))
  ) {
    node = parent;
    parent = node.getParent();
  }
  if (!parent) return false;
  if (Node.isReturnStatement(parent) || Node.isArrowFunction(parent)) return true;
  return Node.isVariableDeclaration(parent) && !parent.getFirstAncestor(isFunctionLike);
}

function requestFrom(obj: ObjectLiteralExpression): Endpoint | null {
  let url: string | null = null;
  let method: string | null = null;
  let hasBody = false;
  let dataType: Type | null = null;
  let paramsType: Type | null = null;
  let inputType = "";
  let outputType = "";
  let timeout = "";
  let keepalive = false;
  let credentials = "";
  const silencedErrors: string[] = [];

  for (const prop of obj.getProperties()) {
    if (Node.isShorthandPropertyAssignment(prop)) {
      switch (prop.getName()) {
        case "url":
          url = resolveDeclaredString(prop.getValueSymbol());
          break;
        case "data":
          hasBody = true;
          dataType = prop.getType();
          break;
        case "params":
          paramsType = prop.getType();
          break;
      }
      continue;
    }
    if (!Node.isPropertyAssignment(prop)) continue;

    const init = prop.getInitializer();
    if (!init) continue;

    switch (prop.getName()) {
      case "url":
        url = resolveStringValue(init);
        break;
      case "method":
        method = resolveStringValue(init)?.toLowerCase() ?? null;
        break;
      case "data":
        hasBody = true;
        dataType = init.getType();
        break;
      case "params":
        paramsType = init.getType();
        break;
      case "input":
        inputType = resolveStringValue(init) ?? "";
        break;
      case "output":
        outputType = resolveStringValue(init) ?? "";
        break;
      case "timeout":
        timeout = resolveConstantValue(init);
        break;
      case "keepalive":
        keepalive = init.getText() === "true";
        break;
      case "credentials":
        credentials = resolveStringValue(init) ?? "";
        break;
      case "silence":
        silencedErrors.push(...resolveSilenceArray(init));
        break;
    }
  }

  if (!url || !method || !METHODS.has(method)) return null;
  const route = routeOf(url);
  if (!route) return null;
  const declaration = declarationOf(obj);
  if (!declaration) return null;

  const { name, node, fn } = declaration;
  const description = extractJsDoc(fn, node);

  return {
    name,
    method,
    url: route,
    tag: tagFromUrl(route),
    description,
    deprecated: description.toLowerCase().includes("@deprecated") || hasDeprecatedTag(fn, node),
    isPublic:
      credentials === "omit" ||
      description.toLowerCase().includes("public") ||
      hasPublicComment(fn),
    pathParams: extractPathParams(route),
    queryParams: paramsType ? typeToProperties(paramsType) : [],
    bodyParams: dataType ? typeToProperties(dataType) : [],
    hasBody,
    inputType,
    outputType,
    timeout,
    keepalive,
    silencedErrors,
  };
}

// ── The name a request goes by ──

interface Declaration {
  name: string;
  node: Node;
  fn: Node;
}

// A request is named after the function that sends it, or the constant that
// holds it. A local value on the way - `const response = await api(...)` - is
// not a name for it.
function declarationOf(obj: Node): Declaration | null {
  for (const node of obj.getAncestors()) {
    const name = nameOf(node);
    if (name) return { name, node, fn: obj.getFirstAncestor(isFunctionLike) ?? node };
  }
  return null;
}

function nameOf(node: Node): string | undefined {
  if (Node.isFunctionDeclaration(node) || Node.isMethodDeclaration(node)) return node.getName();
  if (
    Node.isVariableDeclaration(node) ||
    Node.isPropertyAssignment(node) ||
    Node.isPropertyDeclaration(node)
  ) {
    if (!Node.isIdentifier(node.getNameNode())) return undefined;
    const init = node.getInitializer();
    if ((init && isFunctionLike(init)) || !node.getFirstAncestor(isFunctionLike)) {
      return node.getName();
    }
  }
  return undefined;
}

function isFunctionLike(node: Node): boolean {
  return (
    Node.isArrowFunction(node) ||
    Node.isFunctionDeclaration(node) ||
    Node.isFunctionExpression(node) ||
    Node.isMethodDeclaration(node)
  );
}

// ── String/constant resolution ──

function resolveStringValue(node: Node): string | null {
  if (Node.isStringLiteral(node)) return node.getLiteralText();
  if (Node.isIdentifier(node)) return resolveDeclaredString(node.getSymbol());
  if (Node.isNoSubstitutionTemplateLiteral(node)) return node.getLiteralText();

  if (Node.isTemplateExpression(node)) {
    let result = node.getHead().getLiteralText();
    for (const span of node.getTemplateSpans()) {
      const expr = span.getExpression();
      if (opensQuery(expr)) return result + "?";
      const declared = Node.isIdentifier(expr) ? resolveDeclaredString(expr.getSymbol()) : null;
      result += declared ?? `{${placeholderName(expr)}}`;
      result += span.getLiteral().getLiteralText();
    }
    return result;
  }

  return null;
}

// resolveDeclaredString is the string a name was declared as, followed through
// an import to wherever it is declared.
function resolveDeclaredString(symbol: TsSymbol | undefined): string | null {
  const target = symbol?.isAlias() ? symbol.getAliasedSymbol() : symbol;
  const decl = target?.getValueDeclaration();
  const init = decl && Node.isVariableDeclaration(decl) ? decl.getInitializer() : undefined;
  return init ? resolveStringValue(init) : null;
}

// `${page ? `?${page}` : ''}` adds a query string when there is one.
function opensQuery(expr: Node): boolean {
  if (!Node.isConditionalExpression(expr)) return false;
  return [expr.getWhenTrue(), expr.getWhenFalse()].some((branch) =>
    resolveStringValue(branch)?.startsWith("?")
  );
}

// A placeholder is named after the value that fills it: `${dto.shareId}` and
// `${encodeURIComponent(shareId)}` are both {shareId}.
function placeholderName(expr: Node): string {
  if (Node.isIdentifier(expr)) return expr.getText();
  if (Node.isPropertyAccessExpression(expr)) return expr.getName();
  if (Node.isCallExpression(expr)) {
    const [first] = expr.getArguments();
    if (first) return placeholderName(first);
    const callee = expr.getExpression();
    if (Node.isPropertyAccessExpression(callee)) return placeholderName(callee.getExpression());
  }
  if (Node.isAsExpression(expr) || Node.isNonNullExpression(expr) || Node.isParenthesizedExpression(expr)) {
    return placeholderName(expr.getExpression());
  }
  return expr.getText();
}

function resolveConstantValue(node: Node): string {
  if (Node.isNumericLiteral(node)) return node.getLiteralText();
  if (Node.isIdentifier(node)) {
    let symbol = node.getSymbol();
    if (symbol?.isAlias()) symbol = symbol.getAliasedSymbol();
    const decl = symbol?.getValueDeclaration();
    const init = decl && Node.isVariableDeclaration(decl) ? decl.getInitializer() : undefined;
    if (init && Node.isNumericLiteral(init)) return init.getLiteralText();
    return resolveDeclaredString(symbol) || node.getText();
  }
  return node.getText();
}

function resolveSilenceArray(node: Node): string[] {
  const errors: string[] = [];
  if (Node.isArrayLiteralExpression(node)) {
    for (const el of node.getElements()) {
      errors.push(el.getText());
    }
  }
  if (node.getText() === "true") {
    errors.push("all");
  }
  return errors;
}

// ── URL handling ──

// routeOf is the path a URL names on the API, or null for one that is not on it:
// an absolute address, or a path whose root is only known at run time.
function routeOf(url: string): string | null {
  if (/^[a-z][a-z0-9+.-]*:/i.test(url)) return null;
  const route = "/" + url.replace(/\?.*$/, "").replace(/^\//, "");
  return route.startsWith("/{") ? null : route;
}

function tagFromUrl(url: string): string {
  const first = url.replace(/^\//, "").split("/")[0];
  return first.charAt(0).toUpperCase() + first.slice(1);
}

function extractPathParams(url: string): string[] {
  return [...url.matchAll(/\{(\w+)\}/g)].map((m) => m[1]);
}

// ── JSDoc / comments ──

function extractJsDoc(fnNode: Node, declNode: Node): string {
  for (const n of [fnNode, declNode, declNode.getParent(), declNode.getParent()?.getParent()]) {
    if (!n) continue;
    const text = getJsDocText(n);
    if (text) return text;
  }
  return "";
}

function getJsDocText(node: Node): string {
  const jsDocs = (node as any).getJsDocs?.();
  if (!jsDocs || jsDocs.length === 0) return "";
  return jsDocs.map((d: any) => d.getDescription?.() ?? "").join(" ").trim();
}

function hasPublicComment(node: Node): boolean {
  const sf = node.getSourceFile();
  const start = node.getStart();
  const textBefore = sf.getFullText().slice(Math.max(0, start - 200), start);
  return /\/\*\*?\s*[Pp]ublic\s*\*\*?\/\s*$/.test(textBefore);
}

function hasDeprecatedTag(fnNode: Node, declNode: Node): boolean {
  for (const n of [fnNode, declNode, declNode.getParent(), declNode.getParent()?.getParent()]) {
    if (!n) continue;
    const jsDocs = (n as any).getJsDocs?.();
    if (!jsDocs) continue;
    for (const d of jsDocs) {
      const tags = d.getTags?.();
      if (tags?.some((t: any) => t.getTagName() === "deprecated")) return true;
    }
  }
  return false;
}
