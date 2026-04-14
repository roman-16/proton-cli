import { SyntaxKind, Node, type ArrowFunction, type FunctionDeclaration } from "ts-morph";
import type { Endpoint } from "./types.js";
import { STRING_CONSTANTS, NUMBER_CONSTANTS } from "./registry.js";
import { findArrowFunction, extractDataParams, extractQueryParams } from "./extract-params.js";

/**
 * Extract an endpoint from an arrow function variable declaration.
 */
export function extractFromArrow(name: string, node: Node, decl: Node): Endpoint | null {
  const arrow = findArrowFunction(node);
  if (!arrow) return null;

  const returnObj = findReturnObject(arrow);
  if (!returnObj) return null;

  return buildEndpoint(name, returnObj, arrow, decl);
}

/**
 * Extract an endpoint from a function declaration.
 */
export function extractFromFunction(name: string, decl: FunctionDeclaration): Endpoint | null {
  const body = decl.getBody();
  if (!body || !Node.isBlock(body)) return null;

  const ret = body.getFirstDescendantByKind(SyntaxKind.ReturnStatement);
  if (!ret) return null;
  const expr = ret.getExpression();
  if (!expr || !Node.isObjectLiteralExpression(expr)) return null;

  return buildEndpoint(name, expr, decl, decl);
}

// ── Return object discovery ──

function findReturnObject(arrow: ArrowFunction): Node | null {
  const body = arrow.getBody();

  if (Node.isParenthesizedExpression(body)) {
    const inner = body.getExpression();
    if (Node.isObjectLiteralExpression(inner)) return inner;
  }
  if (Node.isObjectLiteralExpression(body)) return body;
  if (Node.isBlock(body)) {
    for (const stmt of body.getStatements()) {
      if (Node.isReturnStatement(stmt)) {
        const expr = stmt.getExpression();
        if (expr && Node.isObjectLiteralExpression(expr)) return expr;
      }
    }
    const ret = body.getFirstDescendantByKind(SyntaxKind.ReturnStatement);
    if (ret) {
      const expr = ret.getExpression();
      if (expr && Node.isObjectLiteralExpression(expr)) return expr;
    }
  }
  return null;
}

// ── Build endpoint from return object ──

function buildEndpoint(name: string, obj: Node, fnNode: Node, declNode: Node): Endpoint | null {
  if (!Node.isObjectLiteralExpression(obj)) return null;

  let url: string | null = null;
  let method: string | null = null;
  let hasBody = false;
  let hasParams = false;
  let paramsObj: Node | null = null;
  let inputType = "";
  let outputType = "";
  let timeout = "";
  let keepalive = false;
  let credentials = "";
  const silencedErrors: string[] = [];

  for (const prop of obj.getProperties()) {
    if (Node.isShorthandPropertyAssignment(prop)) {
      const propName = prop.getName();
      if (propName === "data") hasBody = true;
      if (propName === "params") hasParams = true;
      continue;
    }
    if (Node.isSpreadAssignment(prop)) continue;
    if (!Node.isPropertyAssignment(prop)) continue;

    const propName = prop.getName();
    const init = prop.getInitializer();
    if (!init) continue;

    switch (propName) {
      case "url":
        url = resolveStringValue(init);
        break;
      case "method":
        method = resolveStringValue(init)?.toLowerCase() ?? null;
        break;
      case "data":
        hasBody = true;
        break;
      case "params":
        hasParams = true;
        paramsObj = init;
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

  if (!url || !method) return null;

  const normalizedUrl = normalizeUrl(url);
  const pathParams = extractPathParams(url);
  const description = extractJsDoc(fnNode, declNode);

  const isPublic =
    credentials === "omit" ||
    description.toLowerCase().includes("public") ||
    hasPublicComment(fnNode);

  const deprecated =
    description.toLowerCase().includes("@deprecated") ||
    hasDeprecatedTag(fnNode, declNode);

  const bodyParams = hasBody ? extractDataParams(fnNode) : [];
  const queryParams = hasParams && paramsObj ? extractQueryParams(paramsObj) : [];

  return {
    name,
    method,
    url: normalizedUrl,
    tag: tagFromUrl(normalizedUrl),
    description,
    deprecated,
    isPublic,
    pathParams,
    queryParams,
    bodyParams,
    hasBody,
    inputType,
    outputType,
    timeout,
    keepalive,
    silencedErrors,
  };
}

// ── String/constant resolution ──

function resolveStringValue(node: Node): string | null {
  if (Node.isStringLiteral(node)) return node.getLiteralText();
  if (Node.isNoSubstitutionTemplateLiteral(node)) return node.getLiteralText();

  if (Node.isTemplateExpression(node)) {
    let result = node.getHead().getLiteralText();
    for (const span of node.getTemplateSpans()) {
      const expr = span.getExpression();
      if (Node.isIdentifier(expr)) {
        const val = STRING_CONSTANTS.get(expr.getText());
        result += val ?? `{${expr.getText()}}`;
      } else {
        result += `{${expr.getText()}}`;
      }
      result += span.getLiteral().getLiteralText();
    }
    return result;
  }

  return null;
}

function resolveConstantValue(node: Node): string {
  if (Node.isNumericLiteral(node)) return node.getLiteralText();
  if (Node.isIdentifier(node)) {
    const name = node.getText();
    const num = NUMBER_CONSTANTS.get(name);
    if (num !== undefined) return String(num);
    const str = STRING_CONSTANTS.get(name);
    if (str) return str;
    return name;
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

function normalizeUrl(url: string): string {
  let n = url.replace(/\?.*$/, "");
  if (!n.startsWith("/")) n = "/" + n;
  return n;
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
