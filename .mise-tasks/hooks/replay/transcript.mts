// Turns a Claude Code session transcript (the ~/.claude/projects/<project>/
// <session>.jsonl tree) into the deliveries the three capture paths would have
// made for it: Claude hook payloads, Anthropic inference frames and Claude Code
// OTel log events. Pure functions only; the hooks:replay task does the I/O.
//
// The transcript is a tree: every message record carries `uuid` and
// `parentUuid`, and a rewind leaves a fork (two prompt records under one
// parent). Each `api-request` record describes one API call as a delta over
// the previous one: `keep` leading messages reused, then the `tail` hashes
// appended; `api-request-blob` records hold the message behind each hash. That
// list is exactly what Anthropic's inference hook would have delivered.

import { createHash, createHmac, randomUUID } from "node:crypto";

export interface TranscriptMessage {
  id?: string;
  role?: string;
  model?: string;
  content?: unknown;
  usage?: Record<string, number>;
}

export interface TranscriptRecord {
  type: string;
  uuid?: string;
  parentUuid?: string | null;
  sessionId?: string;
  timestamp?: string;
  cwd?: string;
  isMeta?: boolean;
  isSidechain?: boolean;
  promptId?: string;
  toolUseResult?: unknown;
  message?: TranscriptMessage;
  // api-request
  id?: string;
  keep?: number;
  tail?: string[];
  params?: { model?: string };
  // api-request-blob
  hash?: string;
}

export interface ContentBlock {
  type?: string;
  text?: string;
  id?: string;
  name?: string;
  input?: unknown;
  tool_use_id?: string;
  content?: unknown;
  is_error?: boolean;
}

export interface FrameMessage {
  role: string;
  content: ContentBlock[];
}

export type HookEvent =
  | "UserPromptSubmit"
  | "PreToolUse"
  | "PostToolUse"
  | "Stop";

export interface HookStep {
  kind: "hook";
  event: HookEvent;
  payload: Record<string, unknown>;
}

export interface FrameStep {
  kind: "frame";
  requestId: string;
  model: string;
  messages: FrameMessage[];
}

export interface OtelStep {
  kind: "otel";
  body: string;
  timeUnixNano: string;
  attributes: Record<string, string | number | boolean>;
}

export type ReplayStep = (HookStep | FrameStep | OtelStep) & {
  // at is the record's timestamp; replay keeps file order regardless.
  at: string;
  recordIndex: number;
};

export interface ReplayPlan {
  sessionId: string;
  cwd: string;
  steps: ReplayStep[];
  forks: Fork[];
  warnings: string[];
}

export interface Fork {
  parentUuid: string;
  promptUuids: string[];
}

export interface PlanOptions {
  transcriptPath: string;
}

export function parseTranscript(text: string): TranscriptRecord[] {
  const records: TranscriptRecord[] = [];
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    try {
      const parsed: unknown = JSON.parse(trimmed);
      if (parsed && typeof parsed === "object" && "type" in parsed) {
        records.push(parsed as TranscriptRecord);
      }
    } catch {
      // A truncated last line is normal for a session still being written.
    }
  }
  return records;
}

export function blocksOf(content: unknown): ContentBlock[] {
  if (typeof content === "string") {
    return [{ type: "text", text: content }];
  }
  if (!Array.isArray(content)) return [];
  return content.filter(
    (block): block is ContentBlock => !!block && typeof block === "object",
  );
}

export function textOf(content: unknown): string {
  return blocksOf(content)
    .filter((block) => block.type === "text" && typeof block.text === "string")
    .map((block) => block.text)
    .join("\n");
}

function toolResults(record: TranscriptRecord): ContentBlock[] {
  return blocksOf(record.message?.content).filter(
    (block) => block.type === "tool_result",
  );
}

// A prompt is a user record a person typed: not a tool result, not an
// injected meta record, with some text.
export function isPrompt(record: TranscriptRecord): boolean {
  if (record.type !== "user" || record.isMeta || record.isSidechain)
    return false;
  if (record.toolUseResult !== undefined || toolResults(record).length > 0) {
    return false;
  }
  return textOf(record.message?.content).trim() !== "";
}

// forks lists every parent with more than one prompt child: the shape a
// rewind leaves behind.
export function findForks(records: TranscriptRecord[]): Fork[] {
  const promptsByParent = new Map<string, string[]>();
  for (const record of records) {
    if (!record.uuid || !record.parentUuid || !isPrompt(record)) continue;
    const kids = promptsByParent.get(record.parentUuid) ?? [];
    kids.push(record.uuid);
    promptsByParent.set(record.parentUuid, kids);
  }
  const forks: Fork[] = [];
  for (const [parentUuid, promptUuids] of promptsByParent) {
    if (promptUuids.length > 1) forks.push({ parentUuid, promptUuids });
  }
  return forks;
}

function nanoString(timestamp: string | undefined, fallbackMs: number): string {
  const ms = timestamp ? Date.parse(timestamp) : Number.NaN;
  const value = Number.isNaN(ms) ? fallbackMs : ms;
  return (BigInt(value) * 1000000n).toString();
}

function toolResultText(content: unknown): string {
  if (typeof content === "string") return content;
  return textOf(content);
}

export function planReplay(
  records: TranscriptRecord[],
  options: PlanOptions,
): ReplayPlan {
  const sessionId = records.find((r) => r.sessionId)?.sessionId ?? "";
  const cwd = records.find((r) => r.cwd)?.cwd ?? "";
  const warnings: string[] = [];
  const steps: ReplayStep[] = [];

  const blobs = new Map<string, TranscriptMessage>();
  for (const record of records) {
    if (record.type === "api-request-blob" && record.hash && record.message) {
      blobs.set(record.hash, record.message);
    }
  }

  const toolUses = new Map<string, { name: string; input: unknown }>();
  const seenUsage = new Set<string>();
  let lastPromptId = "";
  let previousList: FrameMessage[] = [];
  const base = (record: TranscriptRecord): Record<string, unknown> => ({
    session_id: sessionId,
    transcript_path: options.transcriptPath,
    cwd: record.cwd ?? cwd,
    permission_mode: "default",
  });
  const otelBase = (record: TranscriptRecord) => ({
    "session.id": sessionId,
    "event.timestamp": record.timestamp ?? new Date().toISOString(),
    ...(lastPromptId ? { "prompt.id": lastPromptId } : {}),
  });

  records.forEach((record, recordIndex) => {
    const at = record.timestamp ?? "";
    if (record.isSidechain) return;

    if (isPrompt(record)) {
      const prompt = textOf(record.message?.content);
      lastPromptId = record.promptId ?? "";
      steps.push({
        kind: "hook",
        event: "UserPromptSubmit",
        payload: {
          ...base(record),
          hook_event_name: "UserPromptSubmit",
          prompt,
          ...(record.promptId ? { prompt_id: record.promptId } : {}),
        },
        at,
        recordIndex,
      });
      steps.push({
        kind: "otel",
        body: "claude_code.user_prompt",
        timeUnixNano: nanoString(record.timestamp, recordIndex),
        attributes: {
          ...otelBase(record),
          "event.name": "user_prompt",
          prompt_length: prompt.length,
          prompt,
        },
        at,
        recordIndex,
      });
      return;
    }

    if (record.type === "user") {
      for (const result of toolResults(record)) {
        const toolUseId = result.tool_use_id ?? "";
        const use = toolUses.get(toolUseId);
        if (!use) {
          warnings.push(`tool result ${toolUseId} has no recorded tool call`);
        }
        const response = toolResultText(result.content);
        steps.push({
          kind: "hook",
          event: "PostToolUse",
          payload: {
            ...base(record),
            hook_event_name: "PostToolUse",
            tool_name: use?.name ?? "",
            tool_input: use?.input ?? {},
            tool_use_id: toolUseId,
            tool_response: response,
          },
          at,
          recordIndex,
        });
        steps.push({
          kind: "otel",
          body: "claude_code.tool_result",
          timeUnixNano: nanoString(record.timestamp, recordIndex),
          attributes: {
            ...otelBase(record),
            "event.name": "tool_result",
            tool_name: use?.name ?? "",
            success: result.is_error !== true,
          },
          at,
          recordIndex,
        });
      }
      return;
    }

    if (record.type === "assistant") {
      const blocks = blocksOf(record.message?.content);
      const text = textOf(record.message?.content);
      let hadToolUse = false;
      for (const block of blocks) {
        if (block.type !== "tool_use" || !block.id) continue;
        hadToolUse = true;
        toolUses.set(block.id, { name: block.name ?? "", input: block.input });
        steps.push({
          kind: "hook",
          event: "PreToolUse",
          payload: {
            ...base(record),
            hook_event_name: "PreToolUse",
            tool_name: block.name ?? "",
            tool_input: block.input ?? {},
            tool_use_id: block.id,
          },
          at,
          recordIndex,
        });
      }
      // Claude Code fires Stop once the turn ends: a reply with no tool call.
      if (!hadToolUse && text.trim() !== "") {
        steps.push({
          kind: "hook",
          event: "Stop",
          payload: {
            ...base(record),
            hook_event_name: "Stop",
            stop_hook_active: false,
            last_assistant_message: text,
          },
          at,
          recordIndex,
        });
      }
      // One API response is split into one record per block; its usage is
      // reported once.
      const usage = record.message?.usage;
      const messageId = record.message?.id ?? record.uuid ?? "";
      if (usage && !seenUsage.has(messageId)) {
        seenUsage.add(messageId);
        steps.push({
          kind: "otel",
          body: "claude_code.api_request",
          timeUnixNano: nanoString(record.timestamp, recordIndex),
          attributes: {
            ...otelBase(record),
            "event.name": "api_request",
            model: record.message?.model ?? "",
            input_tokens: usage.input_tokens ?? 0,
            output_tokens: usage.output_tokens ?? 0,
            cache_read_tokens: usage.cache_read_input_tokens ?? 0,
            cache_creation_tokens: usage.cache_creation_input_tokens ?? 0,
          },
          at,
          recordIndex,
        });
      }
      return;
    }

    if (record.type === "api-request") {
      const keep = Math.max(0, Math.min(record.keep ?? 0, previousList.length));
      const list = previousList.slice(0, keep);
      for (const hash of record.tail ?? []) {
        const blob = blobs.get(hash);
        if (!blob) {
          warnings.push(
            `api-request ${record.id ?? recordIndex} references unknown blob ${hash}`,
          );
          continue;
        }
        list.push({
          role: blob.role ?? "user",
          content: blocksOf(blob.content),
        });
      }
      previousList = list;
      steps.push({
        kind: "frame",
        requestId: record.id ?? randomUUID(),
        model: record.params?.model ?? "",
        messages: list,
        at,
        recordIndex,
      });
    }
  });

  return { sessionId, cwd, steps, forks: findForks(records), warnings };
}

export interface FrameIdentity {
  sessionId: string;
  actorId: string;
  actorEmail: string;
  tenantId: string;
}

// buildFrame is the body of one inference-hook delivery.
export function buildFrame(
  step: FrameStep,
  identity: FrameIdentity,
): Record<string, unknown> {
  return {
    type: "prompt",
    request_id: step.requestId,
    tenant_id: identity.tenantId,
    actor: {
      type: "user",
      id: identity.actorId,
      email_address: identity.actorEmail,
    },
    source: { application: "claude-code" },
    messages: step.messages,
    session_id: identity.sessionId,
    model: step.model,
  };
}

// decodeSigningSecret mirrors the server: a whsec_ prefix over base64 key bytes.
export function decodeSigningSecret(secret: string): Buffer {
  const key = Buffer.from(secret.replace(/^whsec_/, ""), "base64");
  if (key.length < 16) {
    throw new Error("signing secret must decode to at least 16 bytes");
  }
  return key;
}

export function newSigningSecret(bytes: Buffer): string {
  return `whsec_${bytes.toString("base64")}`;
}

// signFrame produces Standard Webhooks headers the way Anthropic signs its
// deliveries: v1,base64(HMAC-SHA256(key, "<id>.<timestamp>.<body>")).
export function signFrame(
  secret: string,
  requestId: string,
  body: string,
  now: Date,
): Record<string, string> {
  const timestamp = Math.floor(now.getTime() / 1000).toString();
  const mac = createHmac("sha256", decodeSigningSecret(secret));
  mac.update(`${requestId}.${timestamp}.`);
  mac.update(body);
  return {
    "webhook-id": requestId,
    "webhook-timestamp": timestamp,
    "webhook-signature": `v1,${mac.digest("base64")}`,
  };
}

// otlpLogsPayload wraps log records in OTLP/JSON the way Claude Code's
// exporter does, so the Claude dialect on the server recognises them.
export function otlpLogsPayload(
  steps: OtelStep[],
  common: Record<string, string>,
): Record<string, unknown> {
  const attribute = (key: string, value: string | number | boolean) => {
    if (typeof value === "boolean") return { key, value: { boolValue: value } };
    if (typeof value === "number") {
      return Number.isInteger(value)
        ? { key, value: { intValue: String(value) } }
        : { key, value: { doubleValue: value } };
    }
    return { key, value: { stringValue: value } };
  };
  return {
    resourceLogs: [
      {
        resource: {
          attributes: [
            attribute("service.name", "claude-code"),
            attribute("service.version", "replay"),
          ],
        },
        scopeLogs: [
          {
            scope: {
              name: "com.anthropic.claude_code.events",
              version: "1.0.0",
            },
            logRecords: steps.map((step) => ({
              timeUnixNano: step.timeUnixNano,
              observedTimeUnixNano: step.timeUnixNano,
              severityNumber: 9,
              severityText: "INFO",
              body: { stringValue: step.body },
              attributes: Object.entries({ ...common, ...step.attributes }).map(
                ([key, value]) => attribute(key, value),
              ),
            })),
          },
        ],
      },
    ],
  };
}

const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export const dnsNamespace = "6ba7b810-9dad-11d1-80b4-00c04fd430c8";

// uuid5 is RFC 4122 name-based (SHA-1), matching Go's uuid.NewSHA1.
export function uuid5(namespace: string, name: string | Buffer): string {
  const hash = createHash("sha1");
  hash.update(Buffer.from(namespace.replace(/-/g, ""), "hex"));
  hash.update(name);
  const bytes = hash.digest().subarray(0, 16);
  bytes[6] = (bytes[6]! & 0x0f) | 0x50;
  bytes[8] = (bytes[8]! & 0x3f) | 0x80;
  const hex = bytes.toString("hex");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

// sessionIDToChatID mirrors chat.SessionIDToChatID: a UUID session id is the
// hooks chat id; anything else is hashed under the DNS namespace.
export function sessionIDToChatID(sessionId: string): string {
  if (uuidPattern.test(sessionId)) return sessionId.toLowerCase();
  return uuid5(dnsNamespace, sessionId);
}

// inferenceConversationID mirrors anthropicinference.conversationID for a
// frame that carries an actor and a session id.
export function inferenceConversationID(
  projectId: string,
  identity: FrameIdentity,
): string {
  const actorId = identity.actorId || identity.actorEmail.trim().toLowerCase();
  const name = JSON.stringify([
    identity.tenantId,
    "user",
    actorId,
    identity.sessionId,
  ]);
  return uuid5(projectId, name);
}

export function isLocalServer(serverURL: string): boolean {
  const host = new URL(serverURL).hostname;
  return (
    host === "localhost" ||
    host === "127.0.0.1" ||
    host === "::1" ||
    host === "[::1]"
  );
}
