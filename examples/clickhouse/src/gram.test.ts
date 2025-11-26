import { test, beforeAll, expect } from "vitest";
import { createClient } from "@clickhouse/client-web";
import gram from "./gram.ts";

beforeAll(async () => {
  const host = process.env["CLICKHOUSE_HOST"] || "localhost";
  const port = process.env["CLICKHOUSE_PORT"] || "8124";
  const username = process.env["CLICKHOUSE_USERNAME"] || "gram_user";
  const password = process.env["CLICKHOUSE_PASSWORD"] || "gram_password";
  const database = process.env["CLICKHOUSE_DATABASE"] || "gram_example";

  const client = createClient({
    url: `http://${host}:${port}`,
    username,
    password,
    database,
  });

  try {
    const resultSet = await client.query({
      query: "SELECT COUNT(*) as count FROM TrackPlays",
      format: "JSONEachRow",
    });
    const data = (await resultSet.json()) as Array<{ count: string }>;
    const count = Number(data[0]?.count);

    if (count === 0) {
      throw new Error("TrackPlays table is empty. Run npm run db:seed first.");
    }

    console.log(`✓ TrackPlays table contains ${count.toLocaleString()} rows`);
  } catch (error) {
    throw new Error(
      `Failed to connect to ClickHouse or access TrackPlays data: ${error}\n\n` +
        `Ensure:\n1. Docker container is running (docker compose up -d)\n` +
        `2. Database has been seeded (CLICKHOUSE_* env vars set; npm run db:seed)`,
    );
  } finally {
    await client.close();
  }
});

test("execute_query tool - simple SELECT", async () => {
  const result = await gram.handleToolCall({
    name: "execute_query",
    input: {
      query: "SELECT * FROM TrackPlays LIMIT 5",
    },
  });

  expect(result.ok).toBe(true);
  const json = await result.json();
  expect(json.rows).toBeDefined();
  expect(json.rows.length).toBe(5);
  expect(json.rowCount).toBe(5);

  // Check that rows have expected columns
  const firstRow = json.rows[0];
  expect(firstRow).toHaveProperty("Id");
  expect(firstRow).toHaveProperty("Date");
  expect(firstRow).toHaveProperty("UserId");
  expect(firstRow).toHaveProperty("TrackId");
});

test("execute_query tool - parameterized query with Date", async () => {
  const result = await gram.handleToolCall({
    name: "execute_query",
    input: {
      query: `
        SELECT * FROM TrackPlays
        WHERE Date = {target_date: Date}
        LIMIT {limit: UInt32}
      `,
      params: {
        target_date: "2023-01-01",
        limit: 10,
      },
    },
  });

  expect(result.ok).toBe(true);
  const json = await result.json();
  expect(json.rows).toBeDefined();
  expect(json.rows.length).toBeGreaterThan(0);
  expect(json.rows.length).toBeLessThanOrEqual(10);

  // Verify all rows have the correct date
  for (const row of json.rows) {
    expect(row["Date"]).toBe("2023-01-01");
  }
});

test("execute_query tool - time series aggregation", async () => {
  const result = await gram.handleToolCall({
    name: "execute_query",
    input: {
      query: `
        SELECT
          Date,
          COUNT(*) as play_count,
          COUNT(DISTINCT UserId) as unique_users
        FROM TrackPlays
        WHERE Date >= {start_date: Date}
        GROUP BY Date
        ORDER BY Date
        LIMIT 7
      `,
      params: {
        start_date: "2023-01-01",
      },
    },
  });

  expect(result.ok).toBe(true);
  const json = await result.json();
  expect(json.rows).toBeDefined();
  expect(json.rows.length).toBeGreaterThan(0);
  expect(json.rows.length).toBeLessThanOrEqual(7);

  // Check that rows have expected aggregation columns
  const firstRow = json.rows[0];
  if (!firstRow) throw new Error("No rows returned");
  expect(firstRow).toHaveProperty("Date");
  expect(firstRow).toHaveProperty("play_count");
  expect(firstRow).toHaveProperty("unique_users");
  expect(Number(firstRow["play_count"])).toBeGreaterThan(0);
});

test("execute_query tool - JOIN with related tables", async () => {
  const result = await gram.handleToolCall({
    name: "execute_query",
    input: {
      query: `
        SELECT
          t.Name as TrackName,
          ar.Name as ArtistName,
          al.Title as AlbumTitle,
          COUNT(*) as PlayCount
        FROM TrackPlays tp
        JOIN Track t ON tp.TrackId = t.TrackId
        JOIN Album al ON t.AlbumId = al.AlbumId
        JOIN Artist ar ON al.ArtistId = ar.ArtistId
        WHERE tp.Date >= {start_date: Date}
        GROUP BY t.TrackId, t.Name, ar.Name, al.Title
        ORDER BY PlayCount DESC
        LIMIT {limit: UInt32}
      `,
      params: {
        start_date: "2023-01-01",
        limit: 10,
      },
    },
  });

  expect(result.ok).toBe(true);
  const json = await result.json();
  expect(json.rows).toBeDefined();
  expect(json.rows.length).toBeGreaterThan(0);
  expect(json.rows.length).toBeLessThanOrEqual(10);

  // Check that rows have expected JOIN columns
  const firstRow = json.rows[0];
  if (!firstRow) throw new Error("No rows returned");
  expect(firstRow).toHaveProperty("TrackName");
  expect(firstRow).toHaveProperty("ArtistName");
  expect(firstRow).toHaveProperty("AlbumTitle");
  expect(firstRow).toHaveProperty("PlayCount");
  expect(typeof firstRow["TrackName"]).toBe("string");
  expect(typeof firstRow["ArtistName"]).toBe("string");
  expect(typeof firstRow["AlbumTitle"]).toBe("string");
});

test("execute_query tool - UUID filtering", async () => {
  // First get a sample UserId
  const sampleResult = await gram.handleToolCall({
    name: "execute_query",
    input: {
      query: "SELECT DISTINCT UserId FROM TrackPlays LIMIT 1",
    },
  });

  const sampleJson = await sampleResult.json();
  const userId = sampleJson.rows[0]?.["UserId"];
  expect(userId).toBeDefined();

  // Now filter by that UserId
  const result = await gram.handleToolCall({
    name: "execute_query",
    input: {
      query: `
        SELECT * FROM TrackPlays
        WHERE UserId = {user_id: UUID}
        LIMIT 5
      `,
      params: {
        user_id: userId,
      },
    },
  });

  expect(result.ok).toBe(true);
  const json = await result.json();
  expect(json.rows).toBeDefined();
  expect(json.rows.length).toBeGreaterThan(0);

  // Verify all rows have the correct UserId
  for (const row of json.rows) {
    expect(row["UserId"]).toBe(userId);
  }
});

test("execute_query tool - genre popularity by month", async () => {
  const result = await gram.handleToolCall({
    name: "execute_query",
    input: {
      query: `
        SELECT
          toYYYYMM(tp.Date) as month,
          g.Name as Genre,
          COUNT(*) as plays
        FROM TrackPlays tp
        JOIN Track t ON tp.TrackId = t.TrackId
        JOIN Genre g ON t.GenreId = g.GenreId
        WHERE tp.Date >= {start_date: Date}
        GROUP BY month, g.GenreId, g.Name
        ORDER BY month, plays DESC
        LIMIT 20
      `,
      params: {
        start_date: "2023-01-01",
      },
    },
  });

  expect(result.ok).toBe(true);
  const json = await result.json();
  expect(json.rows).toBeDefined();
  expect(json.rows.length).toBeGreaterThan(0);

  // Check that rows have expected columns
  const firstRow = json.rows[0];
  if (!firstRow) throw new Error("No rows returned");
  expect(firstRow).toHaveProperty("month");
  expect(firstRow).toHaveProperty("Genre");
  expect(firstRow).toHaveProperty("plays");
  expect(typeof firstRow["month"]).toBe("number"); // YYYYMM format
  expect(typeof firstRow["Genre"]).toBe("string");
});

test("execute_query tool - user listening patterns", async () => {
  const result = await gram.handleToolCall({
    name: "execute_query",
    input: {
      query: `
        SELECT
          UserId,
          COUNT(*) as total_plays,
          COUNT(DISTINCT TrackId) as unique_tracks,
          MIN(Date) as first_play,
          MAX(Date) as last_play
        FROM TrackPlays
        GROUP BY UserId
        ORDER BY total_plays DESC
        LIMIT {limit: UInt32}
      `,
      params: {
        limit: 10,
      },
    },
  });

  expect(result.ok).toBe(true);
  const json = await result.json();
  expect(json.rows).toBeDefined();
  expect(json.rows.length).toBe(10);

  // Check that rows have expected columns
  const firstRow = json.rows[0];
  expect(firstRow).toHaveProperty("UserId");
  expect(firstRow).toHaveProperty("total_plays");
  expect(firstRow).toHaveProperty("unique_tracks");
  expect(firstRow).toHaveProperty("first_play");
  expect(firstRow).toHaveProperty("last_play");

  // Verify ordering (most plays first)
  const plays = json.rows.map((row) => Number(row["total_plays"]));
  expect(plays[0] ?? 0).toBeGreaterThanOrEqual(plays[plays.length - 1] ?? 0);
});
