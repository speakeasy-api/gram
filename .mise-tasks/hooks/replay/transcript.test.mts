import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import {
  buildFrame,
  decodeSigningSecret,
  findForks,
  inferenceConversationID,
  isLocalServer,
  newSigningSecret,
  otlpLogsPayload,
  parseTranscript,
  planReplay,
  sessionIDToChatID,
  signFrame,
  uuid5,
  type FrameStep,
  type HookStep,
  type OtelStep,
} from "./transcript.mts";

const fixturePath = fileURLToPath(
  new URL("./fixtures/session.jsonl", import.meta.url),
);
const records = parseTranscript(readFileSync(fixturePath, "utf8"));
const plan = planReplay(records, { transcriptPath: fixturePath });
const hooks = plan.steps.filter(
  (s): s is HookStep & typeof s => s.kind === "hook",
);
const frames = plan.steps.filter(
  (s): s is FrameStep & typeof s => s.kind === "frame",
);
const otel = plan.steps.filter(
  (s): s is OtelStep & typeof s => s.kind === "otel",
);

test("the fixture parses into a session with one fork", () => {
  assert.equal(plan.sessionId, "11111111-1111-4111-8111-111111111111");
  assert.equal(plan.cwd, "/replay/workspace");
  assert.deepEqual(plan.warnings, []);
  assert.deepEqual(findForks(records), [
    {
      parentUuid: "aaaa0004-0000-4000-8000-000000000004",
      promptUuids: [
        "aaaa0005-0000-4000-8000-000000000005",
        "aaaa0007-0000-4000-8000-000000000007",
      ],
    },
  ]);
});

test("hook payloads follow the transcript in order", () => {
  assert.deepEqual(
    hooks.map((s) => s.event),
    [
      "UserPromptSubmit",
      "PreToolUse",
      "PostToolUse",
      "Stop",
      "UserPromptSubmit",
      "Stop",
      "UserPromptSubmit",
      "Stop",
    ],
  );
  const [prompt, pre, post] = hooks;
  assert.equal(prompt!.payload.prompt, "REPLAY read the fixture file");
  assert.equal(prompt!.payload.prompt_id, "prompt-0001");
  assert.equal(prompt!.payload.session_id, plan.sessionId);
  assert.equal(prompt!.payload.transcript_path, fixturePath);
  assert.equal(pre!.payload.tool_use_id, "toolu_replay0001");
  assert.equal(pre!.payload.tool_name, "Read");
  assert.deepEqual(pre!.payload.tool_input, {
    file_path: "/replay/workspace/fixture.txt",
  });
  assert.equal(post!.payload.tool_use_id, "toolu_replay0001");
  assert.equal(
    post!.payload.tool_name,
    "Read",
    "the result borrows the call's name",
  );
  assert.equal(post!.payload.tool_response, "fixture body");
  assert.equal(
    hooks[3]!.payload.last_assistant_message,
    "REPLAY the file says: fixture body",
  );
});

test("inference frames rebuild each request from keep and tail", () => {
  assert.deepEqual(
    frames.map((f) => f.messages.length),
    [1, 3, 5, 5],
  );
  assert.deepEqual(
    frames.map((f) => f.requestId),
    ["req-0001", "req-0002", "req-0003", "req-0004"],
  );
  const rewound = frames[3]!;
  assert.equal(rewound.messages[3]!.role, "assistant");
  assert.equal(
    rewound.messages[4]!.content[0]!.text,
    "REPLAY second attempt, kept",
  );
  assert.equal(
    frames[2]!.messages[4]!.content[0]!.text,
    "REPLAY first attempt, abandoned",
    "the abandoned branch was sent before the rewind",
  );
  const frame = buildFrame(rewound, {
    sessionId: plan.sessionId,
    actorId: "actor-1",
    actorEmail: "replay@example.test",
    tenantId: "",
  });
  assert.equal(frame.type, "prompt");
  assert.equal(frame.request_id, "req-0004");
  assert.equal(frame.session_id, plan.sessionId);
  assert.deepEqual(frame.source, { application: "claude-code" });
});

test("OTel events mirror prompts, tool results and API responses", () => {
  assert.deepEqual(
    otel.map((s) => s.attributes["event.name"]),
    [
      "user_prompt",
      "api_request",
      "tool_result",
      "api_request",
      "user_prompt",
      "api_request",
      "user_prompt",
      "api_request",
    ],
  );
  const apiRequest = otel.find(
    (s) => s.attributes["event.name"] === "api_request",
  )!;
  assert.equal(apiRequest.attributes.model, "claude-replay-test");
  assert.equal(apiRequest.attributes.input_tokens, 12);
  assert.equal(apiRequest.attributes["prompt.id"], "prompt-0001");
  const payload = otlpLogsPayload([apiRequest], {
    "user.email": "replay@example.test",
  });
  const logs = (
    payload.resourceLogs as Array<{
      scopeLogs: Array<{ logRecords: Array<Record<string, unknown>> }>;
    }>
  )[0]!.scopeLogs[0]!.logRecords;
  assert.equal(logs.length, 1);
  assert.deepEqual(logs[0]!.body, { stringValue: "claude_code.api_request" });
  const attrs = logs[0]!.attributes as Array<{
    key: string;
    value: Record<string, unknown>;
  }>;
  assert.deepEqual(attrs.find((a) => a.key === "input_tokens")?.value, {
    intValue: "12",
  });
  assert.deepEqual(attrs.find((a) => a.key === "user.email")?.value, {
    stringValue: "replay@example.test",
  });
});

test("ids are derived the way the server derives them", () => {
  // RFC 4122 reference vector.
  assert.equal(
    uuid5("6ba7b810-9dad-11d1-80b4-00c04fd430c8", "python.org"),
    "886313e1-3b8a-5372-9b90-0c9aee199e5d",
  );
  assert.equal(
    sessionIDToChatID("11111111-1111-4111-8111-111111111111"),
    "11111111-1111-4111-8111-111111111111",
  );
  assert.equal(
    sessionIDToChatID("not-a-uuid"),
    uuid5("6ba7b810-9dad-11d1-80b4-00c04fd430c8", "not-a-uuid"),
  );
  const identity = {
    sessionId: "s-1",
    actorId: "",
    actorEmail: "Replay@Example.test",
    tenantId: "",
  };
  assert.equal(
    inferenceConversationID("22222222-2222-4222-8222-222222222222", identity),
    uuid5(
      "22222222-2222-4222-8222-222222222222",
      JSON.stringify(["", "user", "replay@example.test", "s-1"]),
    ),
  );
});

test("frames are signed like Standard Webhooks deliveries", () => {
  const secret = newSigningSecret(Buffer.alloc(32, 7));
  assert.equal(decodeSigningSecret(secret).length, 32);
  const headers = signFrame(
    secret,
    "req-1",
    '{"a":1}',
    new Date(1_800_000_000_000),
  );
  assert.equal(headers["webhook-id"], "req-1");
  assert.equal(headers["webhook-timestamp"], "1800000000");
  assert.match(headers["webhook-signature"]!, /^v1,[A-Za-z0-9+/]+=*$/);
  assert.notEqual(
    headers["webhook-signature"],
    signFrame(secret, "req-2", '{"a":1}', new Date(1_800_000_000_000))[
      "webhook-signature"
    ],
  );
  assert.throws(() => decodeSigningSecret("whsec_c2hvcnQ="));
});

test("only localhost servers are accepted without --allow-remote", () => {
  assert.equal(isLocalServer("https://localhost:8080"), true);
  assert.equal(isLocalServer("http://127.0.0.1:8080"), true);
  assert.equal(isLocalServer("https://app.example.test"), false);
});
