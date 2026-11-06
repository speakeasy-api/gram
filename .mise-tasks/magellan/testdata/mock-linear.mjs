// Preloaded by file.test.mts (`node --import`): replaces fetch with a scripted
// Linear API so the filer runs end to end without the network.
// MOCK_LINEAR_RESPONSES: a JSON array, one entry per request, in order. An
//   entry is a GraphQL body, or {status, body} / {status, raw} for errors.
// MOCK_LINEAR_REQUESTS: each request is appended here as one JSON line.
import { appendFileSync, readFileSync } from "node:fs";

const responses = JSON.parse(
  readFileSync(process.env.MOCK_LINEAR_RESPONSES, "utf8"),
);
let next = 0;

globalThis.fetch = async (url, init = {}) => {
  const { query, variables } = JSON.parse(init.body ?? "{}");
  appendFileSync(
    process.env.MOCK_LINEAR_REQUESTS,
    JSON.stringify({
      url: String(url),
      auth: init.headers?.Authorization,
      query,
      variables,
    }) + "\n",
  );
  const entry = responses[next++];
  if (entry === undefined) throw new Error("mock-linear: unexpected request");
  const scripted = typeof entry.status === "number";
  const status = scripted ? entry.status : 200;
  const text = scripted
    ? (entry.raw ?? JSON.stringify(entry.body))
    : JSON.stringify(entry);
  return new Response(text, {
    status,
    headers: { "content-type": "application/json" },
  });
};
