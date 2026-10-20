import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { isMCPFinding } from "./mcp-finding-context";
import {
  isJudgeSource,
  isLlmAnalyzerSource,
  isShadowMcpSource,
} from "./risk-utils";

export type FindingKind = "mcp" | "chat" | "judge" | "analyzer" | "shadow";

export const FINDING_KIND_LABEL: Record<FindingKind, string> = {
  mcp: "MCP call",
  chat: "Chat message",
  judge: "Prompt policy",
  analyzer: "Risk analyzer",
  shadow: "Shadow MCP",
};

// Shadow MCP is checked first: its identifier finding is about the server, so
// it never gets the payload view even when MCP mediation fields are set.
export function findingKind(result: RiskResult): FindingKind {
  if (isShadowMcpSource(result.source)) return "shadow";
  if (isMCPFinding(result)) return "mcp";
  if (isJudgeSource(result.source)) return "judge";
  if (isLlmAnalyzerSource(result.source)) return "analyzer";
  return "chat";
}

export function isChatLikeKind(kind: FindingKind): boolean {
  return kind === "chat" || kind === "judge" || kind === "analyzer";
}

export type MessageKind =
  | "User prompt"
  | "Assistant"
  | "Tool request"
  | "Tool response"
  | "Message";

function messageKindForField(field: string | undefined): MessageKind | null {
  if (!field) return null;
  if (field === "prompt") return "User prompt";
  if (field === "assistant") return "Assistant";
  if (field === "tool_result") return "Tool response";
  if (field.startsWith("tool.")) return "Tool request";
  return null;
}

// Best effort from the finding alone: the attributed span field when the
// detector reports one, else a tool name implies a tool request.
export function findingMessageKind(result: RiskResult): MessageKind {
  for (const span of result.spans ?? []) {
    const kind = messageKindForField(span.field);
    if (kind) return kind;
  }
  if (result.phase === "response") return "Tool response";
  if (result.toolName) return "Tool request";
  return "Message";
}

// Line two of a chat row's Session · Tool cell, e.g. "Tool request · bash".
export function chatFindingDetail(result: RiskResult): string {
  const kind = findingMessageKind(result);
  return [kind === "Message" ? null : kind, result.toolName]
    .filter(Boolean)
    .join(" · ");
}
