import { Gram } from "@gram-ai/functions";
import { z } from "zod";
import * as snowflake from "./snowflake.ts";

// To learn more about Gram Functions, check out our documentation at:
// https://www.speakeasy.com/docs/gram/gram-functions/functions-framework
const gram = new Gram({
  env: process.env as any,
  envSchema: {
    SNOWFLAKE_ACCOUNT_IDENTIFIER: z
      .string()
      .describe("The Snowflake account identifier."),
    SNOWFLAKE_USER: z.string().describe("The Snowflake username."),
    // SNOWFLAKE_PUBLIC_KEY_FINGERPRINT: z
    //   .string()
    //   .describe("The Snowflake public key fingerprint."),
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
    const requestBody = {
      statement: input.statement,
      bindings: input.bindings,
      timeout: 60,
      database: ctx.env.SNOWFLAKE_DATABASE.toUpperCase(),
      schema: ctx.env.SNOWFLAKE_SCHEMA.toUpperCase(),
      warehouse: ctx.env.SNOWFLAKE_WAREHOUSE.toUpperCase(),
      role: ctx.env.SNOWFLAKE_ROLE.toUpperCase(),
    };

    const bearerToken = snowflake.buildJwt({
      accountIdentifier: ctx.env.SNOWFLAKE_ACCOUNT_IDENTIFIER.toUpperCase(),
      username: ctx.env.SNOWFLAKE_USER.toUpperCase(),
      privateKey: ctx.env.SNOWFLAKE_PRIVATE_KEY,
    });

    const result = await fetch(
      `https://${ctx.env.SNOWFLAKE_ACCOUNT_IDENTIFIER}.snowflakecomputing.com/api/v2/statements`,
      {
        method: "POST",
        headers: {
          Authorization: `Bearer ${bearerToken}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify(requestBody),
      },
    );

    const resultJson = await result.json();

    return ctx.json(resultJson);
  },
});

export default gram;
