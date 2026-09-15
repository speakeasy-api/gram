// Scripted OpenAI-compatible model so Pi can be driven headless with no real
// LLM. Turn 1 emits a tool call against a Gram-bridged tool; turn 2 (once a
// tool result is in the transcript) emits a final text answer.
import { createServer } from "node:http";

const PORT = Number(process.env.PORT ?? 8932);
const TOOL_NAME = process.env.SCRIPT_TOOL ?? "crm_create_task";
const TOOL_ARGS =
  process.env.SCRIPT_ARGS ??
  JSON.stringify({ project: "Apollo Migration", title: "Wire up Gram" });
const requests = [];

function readBody(req) {
  return new Promise((resolve, reject) => {
    let raw = "";
    req.on("data", (c) => (raw += c));
    req.on("end", () => resolve(raw));
    req.on("error", reject);
  });
}

function sse(res, payload) {
  res.write(`data: ${JSON.stringify(payload)}\n\n`);
}

createServer(async (req, res) => {
  if (req.url === "/requests") {
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify(requests));
    return;
  }
  if (req.url?.endsWith("/models")) {
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ data: [{ id: "scripted-1" }] }));
    return;
  }

  const body = JSON.parse((await readBody(req)) || "{}");
  const toolNames = (body.tools ?? [])
    .map((t) => t.function?.name)
    .filter(Boolean);
  const sawToolResult = (body.messages ?? []).some((m) => m.role === "tool");
  requests.push({ toolNames, sawToolResult });
  console.error(
    `[model] turn ${requests.length}: ${toolNames.length} tools offered, sawToolResult=${sawToolResult}`,
  );

  const id = `chatcmpl-${requests.length}`;
  const stream = body.stream === true;

  // Fail loudly rather than silently answering without the bridge: the whole
  // point of the run is that the Gram tool reached the model's tool list.
  if (!sawToolResult && !toolNames.includes(TOOL_NAME)) {
    const message = `bridged tool ${TOOL_NAME} absent from tool list: [${toolNames.join(", ")}]`;
    res.writeHead(400, { "content-type": "application/json" });
    res.end(JSON.stringify({ error: { message } }));
    return;
  }

  const delta = sawToolResult
    ? { role: "assistant", content: "Done — the Gram tool reported back." }
    : {
        role: "assistant",
        content: null,
        tool_calls: [
          {
            index: 0,
            id: "call_1",
            type: "function",
            function: { name: TOOL_NAME, arguments: TOOL_ARGS },
          },
        ],
      };
  const finish = sawToolResult ? "stop" : "tool_calls";

  if (!stream) {
    res.writeHead(200, { "content-type": "application/json" });
    res.end(
      JSON.stringify({
        id,
        object: "chat.completion",
        created: Math.floor(Date.now() / 1000),
        model: body.model ?? "scripted-1",
        choices: [{ index: 0, message: delta, finish_reason: finish }],
        usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
      }),
    );
    return;
  }

  res.writeHead(200, {
    "content-type": "text/event-stream",
    "cache-control": "no-cache",
    connection: "keep-alive",
  });
  const base = {
    id,
    object: "chat.completion.chunk",
    created: Math.floor(Date.now() / 1000),
    model: body.model ?? "scripted-1",
  };
  sse(res, { ...base, choices: [{ index: 0, delta, finish_reason: null }] });
  sse(res, {
    ...base,
    choices: [{ index: 0, delta: {}, finish_reason: finish }],
    usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
  });
  res.write("data: [DONE]\n\n");
  res.end();
}).listen(PORT, () => console.error(`[model] scripted model on :${PORT}`));
