import Turbopuffer from "@turbopuffer/turbopuffer";
import gram from "./gram.ts";
import { test, beforeAll, expect } from "vitest";

// Validate & load environment variables before running tests
beforeAll(() => {
  if (!process.env["TURBOPUFFER_API_KEY"]) {
    throw new Error(
      "TURBOPUFFER_API_KEY is not defined in environment variables",
    );
  }

  if (!process.env["OPENROUTER_API_KEY"]) {
    throw new Error(
      "OPENROUTER_API_KEY is not defined in environment variables",
    );
  }
});

// Validate the namespace and metadata before running tests
beforeAll(async () => {
  const tpuf = new Turbopuffer({
    apiKey: process.env["TURBOPUFFER_API_KEY"]!,
    region: process.env["TURBOPUFFER_REGION"],
  });

  let ns: Turbopuffer.Namespace;
  try {
    ns = tpuf.namespace("movies");
    await ns.metadata();
  } catch (error) {
    throw new Error(
      `Failed to access 'movies' namespace: ${
        error instanceof Error ? error.message : String(error)
      }\n\nEnsure that the Turbopuffer API key is correct and that the 'movies' namespace exists. You may need to run 'npm run db:seed'`,
    );
  }
});

test("search tool", async () => {
  const query = "Inception";
  const limit = 5;
  const result = await gram.handleToolCall({
    name: "search",
    input: { query, limit },
  });

  expect(result.ok).toBe(true);

  const json = await result.json();
  expect(json.query).toBe(query);
  expect(json.results.length).toBeGreaterThan(0);
  expect(json.results.length).toBeLessThanOrEqual(limit);
});
