import { Gram } from "@gram-ai/functions";
import { z } from "zod";
import * as planetscale from "./planetscale.ts";

// To learn more about Gram Functions, check out our documentation at:
// https://www.speakeasy.com/docs/gram/gram-functions/functions-framework
const gram = new Gram({
  envSchema: {
    PLANETSCALE_HOST: z
      .string()
      .describe("The PlanetScale PostgreSQL host (format: XXXX.pg.psdb.cloud)."),
    PLANETSCALE_PORT: z
      .string()
      .describe("The PlanetScale PostgreSQL port (typically 6432)."),
    PLANETSCALE_DATABASE: z
      .string()
      .describe("The PlanetScale database name."),
    PLANETSCALE_USERNAME: z
      .string()
      .describe("The PlanetScale PostgreSQL username."),
    PLANETSCALE_PASSWORD: z
      .string()
      .describe("The PlanetScale PostgreSQL password (pscale_pw_XXXX format)."),
  },
}).tool({
  name: "execute_query",
  description:
    "Execute a SQL query against a PlanetScale PostgreSQL database. Returns the query results including rows, fields metadata, and affected row counts.",
  inputSchema: {
    query: z
      .string()
      .describe(
        "The SQL query to execute. Use $1, $2, etc. as placeholders for parameterized queries.",
      ),
    args: z
      .optional(
        z.array(z.union([z.string(), z.number(), z.boolean(), z.null()])),
      )
      .describe(
        "Optional array of arguments to bind to the query placeholders. Arguments are bound in order to $1, $2, etc. placeholders.",
      ),
  },
  async execute(ctx, input) {
    const result = await planetscale.executeQuery(
      {
        host: ctx.env.PLANETSCALE_HOST,
        port: ctx.env.PLANETSCALE_PORT,
        database: ctx.env.PLANETSCALE_DATABASE,
        username: ctx.env.PLANETSCALE_USERNAME,
        password: ctx.env.PLANETSCALE_PASSWORD,
      },
      {
        query: input.query,
        args: input.args,
      },
    );

    return ctx.json(result);
  },
});

export default gram;
