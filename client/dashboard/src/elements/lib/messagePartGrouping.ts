import { isToolCallAnnotation } from "./toolCallAnnotation";

export type MessagePartGroup = {
  groupKey: string | undefined;
  indices: number[];
};

type PartLike = { readonly type: string; readonly text?: string };

/**
 * Groups an assistant message's parts for rendering:
 *
 * - Consecutive reasoning parts form a `reasoning-*` group.
 * - A run of tool calls — including the terse annotation text parts the
 *   assistant emits before each batch (see toolCallAnnotation.ts) — forms a
 *   single `tools-*` group, so a multi-step investigation renders as one
 *   block whose heading tracks the latest annotation instead of one box per
 *   batch.
 * - Everything else (real prose, images, …) stays ungrouped and breaks runs.
 */
export function groupAssistantMessageParts(
  parts: readonly PartLike[],
): MessagePartGroup[] {
  const groups: MessagePartGroup[] = [];
  let i = 0;
  while (i < parts.length) {
    if (parts[i]?.type === "reasoning") {
      const indices: number[] = [];
      while (i < parts.length && parts[i]?.type === "reasoning") {
        indices.push(i++);
      }
      groups.push({ groupKey: `reasoning-${indices[0]}`, indices });
      continue;
    }
    if (isToolRunMember(parts, i)) {
      const indices: number[] = [];
      while (i < parts.length && isToolRunMember(parts, i)) {
        indices.push(i++);
      }
      groups.push({ groupKey: `tools-${indices[0]}`, indices });
      continue;
    }
    groups.push({ groupKey: undefined, indices: [i] });
    i++;
  }
  return groups;
}

function isToolRunMember(parts: readonly PartLike[], i: number): boolean {
  const part = parts[i];
  if (!part) return false;
  if (part.type === "tool-call") return true;
  return (
    part.type === "text" &&
    isToolCallAnnotation(part.text ?? "") &&
    parts[i + 1]?.type === "tool-call"
  );
}
