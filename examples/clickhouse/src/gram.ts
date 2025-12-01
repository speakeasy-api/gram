import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";
import * as clickhouse from "./clickhouse.ts";

const gram = new Gram({
  envSchema: {
    CLICKHOUSE_HOST: z.string(),
    CLICKHOUSE_PORT: z.string(),
    CLICKHOUSE_DATABASE: z.string(),
    CLICKHOUSE_USERNAME: z.string(),
    CLICKHOUSE_PASSWORD: z.string(),
  },
}).tool({
  name: "execute_query",
  description:
    "Execute SQL queries against a ClickHouse database containing music play history. " +
    "The database includes TrackPlays time series data (1.4M rows) and related tables " +
    "(Track, Album, Artist, Genre, etc). Use ClickHouse SQL syntax with named parameters " +
    "like {param_name: Type}.",
  inputSchema: {
    query: z.string(),
    params: z.optional(z.any()),
  },
  async execute(ctx, input) {
    const result = await clickhouse.executeQuery(
      {
        host: ctx.env.CLICKHOUSE_HOST,
        port: ctx.env.CLICKHOUSE_PORT,
        database: ctx.env.CLICKHOUSE_DATABASE,
        username: ctx.env.CLICKHOUSE_USERNAME,
        password: ctx.env.CLICKHOUSE_PASSWORD,
      },
      { query: input.query, params: input.params },
    );
    return ctx.json(result);
  },
});

export default gram;
