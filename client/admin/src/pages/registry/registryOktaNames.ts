/** Helpers for editing the Okta namespace of a registry record as text. */
import {
  applyEdits,
  modify,
  parseTree,
  findNodeAtLocation,
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
  const names = findNodeAtLocation(root, ["_meta", OKTA_NAMESPACE, "oinNames"]);
  if (names && names.type !== "array") {
    return { error: "oinNames must be an array before adding to it." };
  }
  if (currentOinNames(text).includes(name)) return { text };
  const edits = modify(
    text,
    ["_meta", OKTA_NAMESPACE, "oinNames", names ? -1 : 0],
    name,
    { formattingOptions: FORMAT, isArrayInsertion: true },
  );
  return { text: applyEdits(text, edits) };
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
