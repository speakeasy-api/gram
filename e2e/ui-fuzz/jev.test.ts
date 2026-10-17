import assert from "node:assert/strict";
import { test } from "node:test";

import {
  DEFAULT_ENDPOINT,
  DEFAULT_MODEL,
  JevClient,
  JevError,
  buildRequest,
  decodeResponse,
  type Question,
} from "./jev.ts";

const questions: Record<string, Question> = {
  state: {
    type: "choice",
    instructions: "which",
    criteria: { a: "A", b: "B" },
  },
  broken: {
    type: "noul",
    instructions: "is it",
    criteria: { true: "yes", false: "no" },
  },
};

test("buildRequest pins the model and rejects malformed noul criteria", () => {
  const req = buildRequest({ x: 1 }, questions);
  assert.equal(req.model, DEFAULT_MODEL);
  assert.deepEqual(req.state, { x: 1 });
  assert.throws(
    () =>
      buildRequest(
        {},
        {
          bad: {
            type: "noul",
            instructions: "",
            criteria: { yes: "", no: "" },
          },
        },
      ),
    /exactly "true" and "false"/,
  );
});

test("decodeResponse requires every question answered with its type", () => {
  const ok = decodeResponse(
    {
      model: "typesafe/jev-1.13",
      answers: {
        state: {
          type: "choice",
          choice: "a",
          probabilities: { a: 0.9, b: 0.1 },
        },
        broken: { type: "noul", noul: 0.05 },
      },
      usage: { input_tokens: 10, output_tokens: 2 },
    },
    questions,
  );
  assert.equal(ok.answers["state"]?.choice, "a");
  assert.throws(
    () =>
      decodeResponse(
        {
          model: "m",
          answers: {
            state: { type: "choice", choice: "a", probabilities: {} },
          },
        },
        questions,
      ),
    (e: unknown) => e instanceof JevError && /broken missing/.test(e.message),
  );
});

test("ask posts a bearer request and surfaces non-200 as JevError", async () => {
  let seen: { url: string; init: RequestInit } | undefined;
  const fetchImpl = (async (
    url: string | URL | Request,
    init?: RequestInit,
  ) => {
    seen = { url: String(url), init: init ?? {} };
    return new Response(
      JSON.stringify({
        model: "typesafe/jev-1.13",
        answers: {
          state: {
            type: "choice",
            choice: "b",
            probabilities: { a: 0.2, b: 0.8 },
          },
          broken: { type: "noul", noul: 0.5 },
        },
        usage: { input_tokens: 1, output_tokens: 1 },
      }),
      { status: 200 },
    );
  }) as typeof fetch;
  const client = new JevClient({ apiKey: "k", fetch: fetchImpl });
  const result = await client.ask({ s: 1 }, questions);
  assert.equal(result.response.answers["state"]?.choice, "b");
  assert.equal(seen?.url, DEFAULT_ENDPOINT);
  assert.equal(seen?.init.method, "POST");
  const headers = seen?.init.headers as Record<string, string>;
  assert.equal(headers["Authorization"], "Bearer k");

  const failing = new JevClient({
    apiKey: "k",
    fetch: (async () => new Response("nope", { status: 402 })) as typeof fetch,
  });
  await assert.rejects(
    failing.ask({}, questions),
    (e: unknown) => e instanceof JevError && e.status === 402,
  );
});
