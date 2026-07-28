// Speakeasy Gram observability plugin for opencode (MVP / demo).
//
// Fires on every tool execution and POSTs a canonical Gram hook event to the
// unified ingest endpoint (POST /rpc/hooks.ingest, schema hook.ingest.v1).
// No backend/frontend changes needed — `source.adapter: "opencode"` is accepted
// as-is and the endpoint is fail-open on missing auth.
//
// Env:
//   GRAM_URL      e.g. https://localhost:8080   (defaults to local dev)
//   GRAM_KEY      an API key WITH the `hooks` scope (grab from the dashboard's
//                 hooks/observability setup — same key the Claude/Cursor plugin uses)
//   GRAM_PROJECT  the project slug
//
// Local dev uses a self-signed cert, so run opencode with
//   NODE_TLS_REJECT_UNAUTHORIZED=0
// (Bun honors it) or point GRAM_URL at a real deployment.

const GRAM_URL = (process.env.GRAM_URL ?? "https://localhost:8080").replace(
  /\/$/,
  "",
);
const GRAM_KEY = process.env.GRAM_KEY ?? "";
const GRAM_PROJECT = process.env.GRAM_PROJECT ?? "";

// One session id per opencode process, in case the hook input omits one.
const fallbackSessionId = crypto.randomUUID();

async function send(
  event: string,
  rawEventName: string,
  session: Record<string, unknown>,
  data: Record<string, unknown>,
) {
  const body = {
    schema_version: "hook.ingest.v1",
    idempotency_key: crypto.randomUUID(),
    source: {
      adapter: "opencode",
      adapter_version: "0.0.1",
      raw_event_name: rawEventName,
    },
    session,
    event: { type: event, occurred_at: new Date().toISOString() },
    data,
  };
  try {
    const res = await fetch(`${GRAM_URL}/rpc/hooks.ingest`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Gram-Key": GRAM_KEY,
        "Gram-Project": GRAM_PROJECT,
        "Idempotency-Key": body.idempotency_key,
      },
      body: JSON.stringify(body),
    });
    console.log(`[gram] ${event} -> ${res.status}`);
  } catch (err) {
    console.log(`[gram] ${event} failed:`, err);
  }
}

export const GramObservability = async ({
  directory,
}: {
  directory: string;
}) => {
  return {
    "session.created": async (input: any) => {
      const session = input?.session ?? input;
      await send(
        "session.started",
        "session.created",
        { id: session?.id ?? fallbackSessionId, cwd: directory },
        {},
      );
    },
    "tool.execute.after": async (input: any, output: any) => {
      await send(
        "tool.completed",
        "tool.execute.after",
        { id: input?.sessionID ?? fallbackSessionId, cwd: directory },
        {
          tool_call: {
            id: input?.callID ?? crypto.randomUUID(),
            name: input?.tool ?? "unknown",
            input: output?.args ?? {},
            output: output?.output ?? output ?? {},
          },
        },
      );
    },
  };
};
