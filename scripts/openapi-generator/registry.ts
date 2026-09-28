import { type SourceFile } from "ts-morph";
import type { EnumInfo } from "./types.js";

export const ENUM_MAP = new Map<string, EnumInfo>();

export function collectEnums(sf: SourceFile): void {
  for (const enumDecl of sf.getEnums()) {
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
