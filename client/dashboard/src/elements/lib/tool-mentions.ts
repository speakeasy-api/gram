export type ToolRecord = Record<string, unknown> | undefined;

export interface MentionableTool {
  id: string;
  name: string;
  description?: string;
}

export interface MentionContext {
  isInMention: boolean;
  query: string;
  atPosition: number;
}

export function toolSetToMentionableTools(
  tools: ToolRecord,
): MentionableTool[] {
  if (!tools) return [];

  return Object.entries(tools).map(([name, tool]) => ({
    id: name,
    name,
    description:
      typeof tool === "object" && tool !== null && "description" in tool
        ? (() => {
            const desc = (tool as { description?: unknown }).description;
            return typeof desc === "string" ? desc : "";
          })()
        : undefined,
  }));
}

export type ComposerSegmentKind = "text" | "tool" | "skill";

export interface ComposerSegment {
  text: string;
  kind: ComposerSegmentKind;
}

/** `@tool` and `/skill` tokens, but only where a reference can start — the
 *  lookbehind is what keeps the `//` in a pasted URL from reading as a skill. */
const TOKEN_PATTERN = /(?<=^|\s)([@/])([\w.-]+)/g;

/**
 * Splits draft text into plain runs and reference runs so the composer can
 * paint each kind in its own color. Only tokens that resolve to something the
 * assistant can act on count — a half-typed `@sla` stays plain until it names a
 * real tool, and a stray `/foo` stays plain unless it names a skill.
 */
export function splitComposerSegments(
  text: string,
  tools: ToolRecord,
  skillNames: readonly string[] = [],
): ComposerSegment[] {
  if (!text) return [];

  const toolNames = new Set(
    Object.keys(tools ?? {}).map((name) => name.toLowerCase()),
  );
  const skills = new Set(skillNames.map((name) => name.toLowerCase()));
  const segments: ComposerSegment[] = [];
  let consumed = 0;
  let match: RegExpExecArray | null;

  TOKEN_PATTERN.lastIndex = 0;
  while ((match = TOKEN_PATTERN.exec(text)) !== null) {
    const kind = referenceKind(match[1]!, match[2]!.toLowerCase(), {
      toolNames,
      skills,
    });
    if (!kind) continue;

    if (match.index > consumed) {
      segments.push({ text: text.slice(consumed, match.index), kind: "text" });
    }
    segments.push({ text: match[0], kind });
    consumed = match.index + match[0].length;
  }

  if (consumed < text.length) {
    segments.push({ text: text.slice(consumed), kind: "text" });
  }

  return segments;
}

function referenceKind(
  sigil: string,
  name: string,
  known: { toolNames: Set<string>; skills: Set<string> },
): ComposerSegmentKind | null {
  if (sigil === "@") return known.toolNames.has(name) ? "tool" : null;
  return known.skills.has(name) ? "skill" : null;
}

/** The `/skill` tokens present in a draft, as skill names. */
export function skillTokensIn(
  text: string,
  skillNames: readonly string[],
): string[] {
  return splitComposerSegments(text, undefined, skillNames)
    .filter((segment) => segment.kind === "skill")
    .map((segment) => segment.text.slice(1));
}

/** Appends a reference token to the draft, spacing it off whatever precedes it. */
export function appendToken(text: string, token: string): string {
  const base = text && !/\s$/.test(text) ? `${text} ` : text;
  return `${base}${token} `;
}

/** Drops a reference token (and the space after it) from the draft. */
export function removeToken(text: string, token: string): string {
  return text
    .replace(new RegExp(`(?<=^|\\s)${escapeForRegExp(token)}\\s?`, "g"), "")
    .trimStart();
}

function escapeForRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function detectMentionContext(
  text: string,
  cursorPosition: number,
): MentionContext {
  const textBeforeCursor = text.slice(0, cursorPosition);
  const lastAtSymbol = textBeforeCursor.lastIndexOf("@");

  if (lastAtSymbol === -1) {
    return { isInMention: false, query: "", atPosition: -1 };
  }

  const textAfterAt = textBeforeCursor.slice(lastAtSymbol + 1);

  if (textAfterAt.includes(" ") || textAfterAt.includes("\n")) {
    return { isInMention: false, query: "", atPosition: -1 };
  }

  return {
    isInMention: true,
    query: textAfterAt.toLowerCase(),
    atPosition: lastAtSymbol,
  };
}

export function filterToolsByQuery(
  tools: MentionableTool[],
  query: string,
): MentionableTool[] {
  if (!query) return tools;

  const queryLower = query.toLowerCase();

  return tools.filter((tool) => {
    const nameMatch = tool.name.toLowerCase().includes(queryLower);
    const descMatch = tool.description?.toLowerCase().includes(queryLower);
    return nameMatch || descMatch;
  });
}

export function insertToolMention(
  text: string,
  toolName: string,
  atPosition: number,
  cursorPosition: number,
): { text: string; cursorPosition: number } {
  const beforeMention = text.slice(0, atPosition);
  const afterCursor = text.slice(cursorPosition);
  const newText = `${beforeMention}@${toolName} ${afterCursor}`;
  const newCursorPosition = atPosition + toolName.length + 2;
  return { text: newText, cursorPosition: newCursorPosition };
}
