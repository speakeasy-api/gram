import assert from "node:assert/strict";
import { test } from "node:test";
import { gatewayEchoes } from "./gateway.ts";

test("progressive gateway discovery describes exact members before executing", async () => {
  const calls: Array<{ name: string; args?: Record<string, unknown> }> = [];
  const result = (value: unknown) => ({
    isError: false,
    structuredContent: value,
  });
  const driver = {
    async listTools() {
      return { tools: [{ name: "list_servers" }, { name: "execute_tool" }] };
    },
    async callTool(name: string, args?: Record<string, unknown>) {
      calls.push({ name, args });
      switch (name) {
        case "list_servers":
          return result({
            servers: [
              { slug: "alpha" },
              { slug: "project.beta" },
              { slug: "unrelated" },
            ],
          });
        case "describe_server":
          return result({ tools: [{ name: `${args?.server}--fixture_echo` }] });
        case "describe_tools":
          return result({
            tools: (args?.tools as string[]).map((tool) => ({
              name: tool,
              inputSchema: { type: "object" },
            })),
          });
        case "execute_tool":
          return result(args);
        default:
          throw new Error("Unexpected gateway tool");
      }
    },
  };
  const tools = await gatewayEchoes(driver, ["alpha", "beta"]);
  assert.deepEqual(
    tools.map((tool) => tool.qualifiedName),
    ["alpha--fixture_echo", "project.beta--fixture_echo"],
  );
  await tools[1]!.call({ marker: "only-beta" });
  assert.deepEqual(calls.at(-1), {
    name: "execute_tool",
    args: {
      name: "project.beta--fixture_echo",
      arguments: { marker: "only-beta" },
    },
  });
  assert.ok(
    !calls.some((call) =>
      JSON.stringify(call.args ?? {}).includes("unrelated"),
    ),
  );
});

test("missing progressive member is distinct from absence of direct tools", async () => {
  const driver = {
    async listTools() {
      return { tools: [{ name: "list_servers" }] };
    },
    async callTool() {
      return { structuredContent: { servers: [] } };
    },
  };
  await assert.rejects(
    gatewayEchoes(driver, ["missing"]),
    /GATEWAY_FILTER_REGRESSION/,
  );
});

test("direct gateway mode invokes the exact qualified member tool", async () => {
  const calls: string[] = [];
  const driver = {
    async listTools() {
      return {
        tools: [
          { name: "alpha--fixture_echo" },
          { name: "beta--fixture_echo" },
        ],
      };
    },
    async callTool(name: string) {
      calls.push(name);
      return { isError: false };
    },
  };
  const tools = await gatewayEchoes(driver, ["beta"]);
  await tools[0]!.call({});
  assert.deepEqual(calls, ["beta--fixture_echo"]);
});
