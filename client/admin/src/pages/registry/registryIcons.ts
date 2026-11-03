import {
  applyEdits,
  findNodeAtLocation,
  getNodeValue,
  modify,
  parseTree,
  type ParseError,
} from "jsonc-parser";
export type CatalogIcon = {
  src: string;
  mimeType: "image/svg+xml";
  sizes: ["any"];
};
export type CatalogIcons = { default?: CatalogIcon; dark?: CatalogIcon };
export type IconSlot = keyof CatalogIcons;
const path = ["_meta", "com.speakeasy.ai/icons"];
const repair = "Fix the JSON before editing icons.";
function editable(text: string) {
  const errors: ParseError[] = [];
  const root = parseTree(text, errors, {
    disallowComments: true,
    allowTrailingComma: false,
  });
  if (!root || root.type !== "object" || errors.length) throw new Error(repair);
  for (const location of [[], ["_meta"], path]) {
    const node = findNodeAtLocation(root, location);
    if (!node) continue;
    const keys = (node.children ?? []).map(
      (child) => child.children?.[0]?.value,
    );
    if (node.type !== "object" || new Set(keys).size !== keys.length)
      throw new Error(repair);
  }
  return root;
}
export function readCatalogIcons(text: string): CatalogIcons {
  try {
    const root = editable(text);
    const icons: CatalogIcons = {};
    for (const slot of ["default", "dark"] as const) {
      const node = findNodeAtLocation(root, [...path, slot]);
      const value = node && getNodeValue(node);
      if (
        value &&
        typeof value.src === "string" &&
        value.mimeType === "image/svg+xml" &&
        Array.isArray(value.sizes) &&
        value.sizes.length === 1 &&
        value.sizes[0] === "any"
      )
        icons[slot] = value;
    }
    return icons;
  } catch {
    return {};
  }
}
export function setCatalogIcon(
  text: string,
  slot: IconSlot,
  icon: CatalogIcon | null,
): string {
  const root = editable(text);
  const namespace = findNodeAtLocation(root, path);
  if (!icon && !findNodeAtLocation(root, [...path, slot])) return text;
  const location =
    !icon && namespace?.children?.length === 1 ? path : [...path, slot];
  return applyEdits(text, modify(text, location, icon ?? undefined, {}));
}
