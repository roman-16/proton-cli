import { Node, type ArrowFunction, type Type } from "ts-morph";
import type { Property } from "./types.js";
import { ENUM_MAP } from "./registry.js";

/**
 * Find the arrow function inside a node (or return the node itself).
 */
export function findArrowFunction(node: Node): ArrowFunction | null {
  if (Node.isArrowFunction(node)) return node;
  return node.getFirstDescendantByKind(74 /* SyntaxKind.ArrowFunction */) as ArrowFunction | null;
}

/**
 * Extract body (data) params from a function/arrow node.
 * Looks for a `data` parameter or the first parameter with useful properties.
 */
export function extractDataParams(fnNode: Node): Property[] {
  let arrow: ArrowFunction | null = null;

  if (Node.isArrowFunction(fnNode)) {
    arrow = fnNode;
  } else if (Node.isVariableDeclaration(fnNode)) {
    const init = fnNode.getInitializer();
    if (init) arrow = findArrowFunction(init);
  }

  if (!arrow) {
    if (Node.isFunctionDeclaration(fnNode)) {
      for (const param of fnNode.getParameters()) {
        if (param.getName() === "data") {
          return typeToProperties(param.getType());
        }
      }
    }
    return [];
  }

  for (const param of arrow.getParameters()) {
    // Direct: (data: SomeType) => ...
    if (param.getName() === "data") {
      return typeToProperties(param.getType());
    }

    // Destructured: ({ data, ...rest }: Type)
    const nameNode = param.getNameNode();
    if (Node.isObjectBindingPattern(nameNode)) {
      for (const element of nameNode.getElements()) {
        if (element.getName() === "data") {
          const dataProp = param.getType().getProperty("data");
          if (dataProp) {
            return typeToProperties(dataProp.getTypeAtLocation(param));
          }
        }
      }
    }
  }

  // Fallback: first param with useful properties (not the config object)
  for (const param of arrow.getParameters()) {
    const props = typeToProperties(param.getType());
    if (props.some((p) => p.name === "url" || p.name === "method")) continue;
    if (props.length > 0) return props;
  }

  return [];
}

/**
 * Extract query params from a params object literal in the return value.
 */
export function extractQueryParams(paramsNode: Node): Property[] {
  if (!Node.isObjectLiteralExpression(paramsNode)) return [];

  return paramsNode
    .getProperties()
    .filter((p) => Node.isPropertyAssignment(p) || Node.isShorthandPropertyAssignment(p))
    .map((p) => ({
      name: (p as any).getName?.() ?? "",
      type: "string",
      optional: false,
      description: "",
    }))
    .filter((p) => p.name);
}

/**
 * Convert a TypeScript type to a list of OpenAPI properties.
 */
export function typeToProperties(type: Type): Property[] {
  const props: Property[] = [];
  try {
    for (const sym of type.getProperties()) {
      const name = sym.getName();
      const valDecl = sym.getValueDeclaration();
      const symType = valDecl ? sym.getTypeAtLocation(valDecl) : sym.getDeclaredType();
      const optional = sym.isOptional();

      let description = "";
      if (valDecl) {
        const jsDocs = (valDecl as any).getJsDocs?.();
        if (jsDocs?.length) {
          description = jsDocs.map((d: any) => d.getDescription?.() ?? "").join(" ").trim();
        }
        if (!description) {
          const trailing = valDecl.getTrailingCommentRanges();
          if (trailing.length > 0) {
            description = trailing[0].getText().replace(/^\/\/\s*/, "").trim();
          }
        }
      }

      props.push({
        name,
        type: tsTypeToOpenApi(symType),
        optional,
        description,
      });
    }
  } catch {
    // Type resolution can fail for complex generics
  }
  return props;
}

/**
 * Map a TypeScript type to an OpenAPI type string.
 */
export function tsTypeToOpenApi(type: Type): string {
  if (type.isString() || type.isStringLiteral()) return "string";
  if (type.isNumber() || type.isNumberLiteral()) return "integer";
  if (type.isBoolean() || type.isBooleanLiteral()) return "boolean";
  if (type.isArray()) return "array";
  if (type.isObject()) return "object";
  if (type.isEnum()) return "integer";
  if (type.isUnion()) {
    const types = type.getUnionTypes();
    if (types.every((t) => t.isNumberLiteral() || t.isNumber())) return "integer";
    if (types.every((t) => t.isStringLiteral() || t.isString())) return "string";
    if (types.every((t) => t.isBooleanLiteral() || t.isBoolean())) return "boolean";
  }
  return "string";
}
