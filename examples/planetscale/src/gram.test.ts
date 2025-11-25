import ws from "ws";
import { Pool, neonConfig } from "@neondatabase/serverless";
import gram from "./gram.ts";
import { test, beforeAll, expect } from "vitest";

// Configure Neon for PlanetScale compatibility
neonConfig.webSocketConstructor = ws;
neonConfig.pipelineConnect = false;
neonConfig.wsProxy = (host, port) => `${host}/v2?address=${host}:${port}`;

// Validate & load environment variables before running tests
beforeAll(() => {
  if (!process.env["PLANETSCALE_HOST"]) {
    throw new Error("PLANETSCALE_HOST is not defined in environment variables");
  }

  if (!process.env["PLANETSCALE_PORT"]) {
    throw new Error("PLANETSCALE_PORT is not defined in environment variables");
  }

  if (!process.env["PLANETSCALE_DATABASE"]) {
    throw new Error(
      "PLANETSCALE_DATABASE is not defined in environment variables",
    );
  }

  if (!process.env["PLANETSCALE_USERNAME"]) {
    throw new Error(
      "PLANETSCALE_USERNAME is not defined in environment variables",
    );
  }

  if (!process.env["PLANETSCALE_PASSWORD"]) {
    throw new Error(
      "PLANETSCALE_PASSWORD is not defined in environment variables",
    );
  }
});

// Validate the database connection and Chinook dataset before running tests
beforeAll(async () => {
  const connectionString = `postgresql://${process.env["PLANETSCALE_USERNAME"]}:${process.env["PLANETSCALE_PASSWORD"]}@${process.env["PLANETSCALE_HOST"]}:${process.env["PLANETSCALE_PORT"]}/${process.env["PLANETSCALE_DATABASE"]}`;
  const pool = new Pool({ connectionString });

  try {
    // Try to query the Artist table to verify Chinook dataset is loaded
    const result = await pool.query("SELECT COUNT(*) as count FROM Artist");
    const count = Number(result.rows[0]?.count);

    if (count === 0) {
      throw new Error(
        "The Artist table exists but is empty. Please load the Chinook dataset.",
      );
    }

    await pool.end();
  } catch (error) {
    throw new Error(
      `Failed to connect to PlanetScale database or access Chinook dataset: ${
        error instanceof Error ? error.message : String(error)
      }\n\nEnsure that:\n1. The PlanetScale credentials are correct\n2. The database is accessible\n3. The Chinook dataset has been loaded (see README.md for instructions)`,
    );
  }
});

test("execute_query tool - simple SELECT", async () => {
  const result = await gram.handleToolCall({
    name: "execute_query",
    input: {
      query: "SELECT * FROM Artist LIMIT 5",
    },
  });

  expect(result.ok).toBe(true);

  const json = await result.json();
  expect(json.rows).toBeDefined();
  expect(json.rows.length).toBeGreaterThan(0);
  expect(json.rows.length).toBeLessThanOrEqual(5);
  expect(json.fields).toBeDefined();
});

test("execute_query tool - parameterized query", async () => {
  const artistName = "AC/DC";
  const result = await gram.handleToolCall({
    name: "execute_query",
    input: {
      query: "SELECT * FROM Artist WHERE Name = $1 LIMIT 1",
      args: [artistName],
    },
  });

  expect(result.ok).toBe(true);

  const json = await result.json();
  expect(json.rows).toBeDefined();
  expect(json.rows.length).toBe(1);
  expect(json.rows[0]).toHaveProperty("name", artistName);
});

test("execute_query tool - JOIN query", async () => {
  const result = await gram.handleToolCall({
    name: "execute_query",
    input: {
      query: `
        SELECT ar.Name as ArtistName, al.Title as AlbumTitle
        FROM Album al
        JOIN Artist ar ON al.ArtistId = ar.ArtistId
        LIMIT 3
      `,
    },
  });

  expect(result.ok).toBe(true);

  const json = await result.json();
  expect(json.rows).toBeDefined();
  expect(json.rows.length).toBe(3);
  // PostgreSQL lowercases unquoted column aliases
  expect(json.rows[0]).toHaveProperty("artistname");
  expect(json.rows[0]).toHaveProperty("albumtitle");
});
