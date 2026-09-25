import {
  applyEdits,
  format,
  parseTree,
  type Node,
  type ParseError,
} from "jsonc-parser";
import type { ValidationIssue } from "./registryValidation";

// Formatting edits only whitespace; parsed numbers/strings never become output.
export function formatRegistryJson(text: string): string | null {
  try {
    JSON.parse(text);
  } catch {
    return null;
  }
  return applyEdits(
    text,
    format(text, undefined, { tabSize: 2, insertSpaces: true, eol: "\n" }),
  );
}

// registry.go joins sanitized validation messages with "; ". Preserve generic
// errors and newlines verbatim; an unrecognized path only gets a root marker.
export function serverValidationIssues(message: string): ValidationIssue[] {
  return message.split(/; (?=\/[^\n]*?: |: )/).map((part) => {
    const match = /^(\/[^\n]*?|): ([\s\S]*)$/.exec(part);
    return match
      ? { path: match[1]!, message: match[2]! }
      : { path: "", message: part };
  });
}

export function registryIssueOffsets(
  text: string,
  issues: ValidationIssue[],
): (ValidationIssue & { offset: number; length: number })[] {
  const errors: ParseError[] = [];
  const root = parseTree(text, errors, {
    disallowComments: true,
    allowTrailingComma: false,
  });
  return issues.map((issue) => {
    let node: Node | undefined = root;
    if (root && errors.length === 0 && issue.path.startsWith("/")) {
      for (const segment of issue.path.slice(1).split("/")) {
        const key = segment.replace(/~1/g, "/").replace(/~0/g, "~");
        const child: Node | undefined =
          node?.type === "object"
            ? node.children?.find(
                (property) => property.children?.[0]?.value === key,
              )?.children?.[1]
            : node?.type === "array" && /^(0|[1-9]\d*)$/.test(key)
              ? node.children?.[Number(key)]
              : undefined;
        if (!child) break;
        node = child;
      }
    }
    return { ...issue, offset: node?.offset ?? 0, length: node?.length ?? 1 };
  });
}
