/** Helpers for editing the Okta namespace of a registry record as text. */
import {
  applyEdits,
  modify,
  parseTree,
  findNodeAtLocation,
  type Node,
} from "jsonc-parser";

export const OKTA_NAMESPACE = "com.speakeasy.ai/okta";

const FORMAT = { tabSize: 2, insertSpaces: true, eol: "\n" };

/**
 * Adds an OIN name to the record's Okta namespace with a source-preserving
 * edit: every other byte of the text, including numbers JavaScript cannot
 * represent, stays exactly as typed. Returns the new text, or an error to
 * show when the text is not an editable JSON object.
 */
export function addOinName(
  text: string,
  name: string,
): { text: string } | { error: string } {
  const root = parseTree(text);
  if (!root || root.type !== "object") {
    return { error: "Fix the JSON before adding an Okta application." };
  }
  // Walk the containers the edit will touch. Each must be an object without
  // duplicate keys: a source edit lands on the first occurrence while every
  // consumer reads the last, so duplicates cannot be edited safely.
  const path: (string | number)[] = [];
  let node: Node | undefined = root;
  for (const key of ["_meta", OKTA_NAMESPACE]) {
    if (!node) break;
    if (node.type !== "object") {
      return { error: `${describe(path)} must be an object.` };
    }
    if (duplicateKey(node)) {
      return { error: `${describe(path)} has a duplicate key; fix it first.` };
    }
    path.push(key);
    node = findNodeAtLocation(root, path);
  }
  const names = node
    ? findNodeAtLocation(root, [...path, "oinNames"])
    : undefined;
  if (node && node.type !== "object") {
    return { error: `${describe(path)} must be an object.` };
  }
  if (node && duplicateKey(node)) {
    return { error: `${describe(path)} has a duplicate key; fix it first.` };
  }
  if (names && names.type !== "array") {
    return { error: "oinNames must be an array before adding to it." };
  }
  if (currentOinNames(text).includes(name)) return { text };
  try {
    const edits = modify(
      text,
      ["_meta", OKTA_NAMESPACE, "oinNames", names ? -1 : 0],
      name,
      { formattingOptions: FORMAT, isArrayInsertion: true },
    );
    return { text: applyEdits(text, edits) };
  } catch {
    return { error: "Fix the JSON before adding an Okta application." };
  }
}

function describe(path: (string | number)[]): string {
  return path.length === 0
    ? "The record"
    : path.map((p) => `["${String(p)}"]`).join("");
}

function duplicateKey(object: Node): boolean {
  const seen = new Set<string>();
  for (const property of object.children ?? []) {
    const key = property.children?.[0]?.value;
    if (typeof key !== "string") continue;
    if (seen.has(key)) return true;
    seen.add(key);
  }
  return false;
}

/** Reads the OIN names currently in the text, if it parses. */
export function currentOinNames(text: string): string[] {
  const root = parseTree(text);
  if (!root) return [];
  const names = findNodeAtLocation(root, ["_meta", OKTA_NAMESPACE, "oinNames"]);
  if (!names || names.type !== "array") return [];
  return (names.children ?? [])
    .filter((n) => n.type === "string")
    .map((n) => n.value as string);
}
