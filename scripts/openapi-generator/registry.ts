import { Node, type SourceFile } from "ts-morph";
import type { EnumInfo } from "./types.js";

export const STRING_CONSTANTS = new Map<string, string>();
export const NUMBER_CONSTANTS = new Map<string, number>();
export const ENUM_MAP = new Map<string, EnumInfo>();

export function collectConstants(sf: SourceFile): void {
  for (const decl of sf.getVariableDeclarations()) {
    const init = decl.getInitializer();
    if (!init) continue;
    if (Node.isStringLiteral(init) || Node.isNoSubstitutionTemplateLiteral(init)) {
      STRING_CONSTANTS.set(decl.getName(), init.getLiteralText());
    }
    if (Node.isNumericLiteral(init)) {
      NUMBER_CONSTANTS.set(decl.getName(), Number(init.getLiteralText()));
    }
  }
}

export function collectEnums(sf: SourceFile): void {
  for (const enumDecl of sf.getEnums()) {
    if (!enumDecl.isExported()) continue;
    const name = enumDecl.getName();
    const values: { key: string; value: string | number }[] = [];
    for (const member of enumDecl.getMembers()) {
      const val = member.getValue();
      if (val !== undefined) {
        values.push({ key: member.getName(), value: val });
      }
    }
    ENUM_MAP.set(name, { name, values });
  }
}
