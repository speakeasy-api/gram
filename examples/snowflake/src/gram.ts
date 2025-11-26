import { Gram } from "@gram-ai/functions";
import { z } from "zod";
import * as snowflake from "./snowflake.ts";

// To learn more about Gram Functions, check out our documentation at:
// https://www.speakeasy.com/docs/gram/gram-functions/functions-framework
const gram = new Gram({
  envSchema: {
    SNOWFLAKE_ACCOUNT_IDENTIFIER: z
      .string()
      .describe("The Snowflake account identifier."),
    SNOWFLAKE_USER: z.string().describe("The Snowflake username."),
    SNOWFLAKE_WAREHOUSE: z.string().describe("The Snowflake warehouse to use."),
    SNOWFLAKE_DATABASE: z
      .string()
      .describe("The Snowflake database to connect to."),
    SNOWFLAKE_SCHEMA: z.string().describe("The Snowflake schema to use."),
    SNOWFLAKE_ROLE: z.string().describe("The Snowflake role to assume."),
    SNOWFLAKE_PRIVATE_KEY: z
      .string()
      .describe(
        "The Snowflake private key. This must correspond to the public key fingerprint.",
      ),
  },
}).tool({
  name: "snowflake_execute_sql",
  description: "Execute a SQL query against a Snowflake database.",
  inputSchema: {
    statement: z.string().describe("The SQL statement to execute."),
    bindings: z.record(
      z
        .string()
        .describe(
          'The key of the binding. Must be a numeric string. Eg: "1", "2", "3".',
        ),
      z.object({
        type: z.enum([
          "FIXED",
          "REAL",
          "TEXT",
          "BINARY",
          "BOOLEAN",
          "DATE",
          "TIME",
          "TIMESTAMP_TZ",
          "TIMESTAMP_LTZ",
          "TIMESTAMP_NTZ",
        ]),
        value: z.string(),
      }),
    ),
  },
  async execute(ctx, input) {
    const result = await snowflake.executeQuery(
      {
        accountIdentifier: ctx.env.SNOWFLAKE_ACCOUNT_IDENTIFIER,
        username: ctx.env.SNOWFLAKE_USER,
        privateKey: ctx.env.SNOWFLAKE_PRIVATE_KEY,
      },
      {
        database: ctx.env.SNOWFLAKE_DATABASE,
        schema: ctx.env.SNOWFLAKE_SCHEMA,
        warehouse: ctx.env.SNOWFLAKE_WAREHOUSE,
        role: ctx.env.SNOWFLAKE_ROLE,
        statement: input.statement,
        bindings: input.bindings,
      },
    );

    return ctx.json(result);
  },
});

export default gram;
