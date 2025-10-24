import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { server } from "./mcp.js";

const [serverTransport, clientTransport] = InMemoryTransport.createLinkedPair();

await server.connect(serverTransport);

const client = new Client({ name: "gram-functions", version: "0.0.0" });
await client.connect(clientTransport);

export async function handleToolCall(call: {
  name: string;
  input?: Record<string, unknown>;
  _meta?: Record<string, unknown>;
}): Promise<Response> {
  const response = await client.callTool({
    name: call.name,
    arguments: call.input,
    _meta: call._meta,
  });

  const body = JSON.stringify(response);
  return new Response(body, {
    status: 200,
    headers: { "Content-Type": "application/json; mcp=tools_call" },
  });
}

export async function handleResources(call: {
  uri: string;
  input: string;
  _meta?: Record<string, unknown>;
}): Promise<Response> {
  const response = await client.readResource({
    uri: call.uri,
    _meta: call._meta,
  });

  const body = JSON.stringify(response);
  return new Response(body, {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
