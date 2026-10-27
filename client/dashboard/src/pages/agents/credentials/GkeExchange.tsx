import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useState, type JSX, type ReactNode } from "react";
import { CodeBlock } from "./CodeBlock";
import { ConsoleRow } from "./ConsoleRow";
import { PendingValues } from "./SetupFields";
import type { SetupServer } from "./setupInstructions";

/**
 * The exchange, run inside the pod each time it needs a Speakeasy token. The
 * access token is scoped to one server, so the exchange names it as the
 * resource and the call goes to that same URL.
 */
function exchangeScript(tokenEndpoint?: string, mcpUrl?: string) {
  return [
    `TOKEN_ENDPOINT=${tokenEndpoint || "SPEAKEASY_TOKEN_ENDPOINT"}`,
    `MCP_URL=${mcpUrl || "MCP_SERVER_URL"}`,
    "",
    "# Read the file each time: Kubernetes replaces the token before it expires.",
    'response=$(curl -sS "$TOKEN_ENDPOINT" \\',
    '  --data-urlencode "grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer" \\',
    '  --data-urlencode "assertion=$(cat "$SPEAKEASY_TOKEN_FILE")" \\',
    '  --data-urlencode "resource=$MCP_URL")',
    "token=$(printf '%s' \"$response\" | jq -r '.access_token // empty')",
    'if [ -z "$token" ]; then echo "Exchange failed: $response" >&2; exit 1; fi',
    "",
    "# Reuse $token until it expires (expires_in seconds), then exchange again.",
    'curl -sS --fail-with-body "$MCP_URL" \\',
    '  -H "Authorization: Bearer $token" \\',
    '  -H "Content-Type: application/json" \\',
    '  -H "Accept: application/json, text/event-stream" \\',
    '  -d \'{"jsonrpc":"2.0","id":1,"method":"tools/list"}\'',
  ].join("\n");
}

/**
 * The exchange step's values: where to exchange the pod's token, and the
 * server the access token is for. An access token reaches one server, so the
 * reader picks which of the agent's servers the snippet targets.
 */
export function ExchangeValues({
  tokenEndpoint,
  servers,
  issuerPicker,
}: {
  tokenEndpoint?: string;
  servers: SetupServer[];
  issuerPicker: ReactNode;
}): JSX.Element {
  const [chosenId, setChosenId] = useState("");
  const server = servers.find((s) => s.id === chosenId) ?? servers[0];
  const placeholders = [
    ...(tokenEndpoint ? [] : ["SPEAKEASY_TOKEN_ENDPOINT"]),
    ...(server ? [] : ["MCP_SERVER_URL"]),
  ];
  return (
    <>
      <p className="text-sm">
        Before calling an MCP server, the workload exchanges its token for a
        Speakeasy access token for that server.
      </p>
      {servers.length > 1 && server !== undefined && (
        <Stack gap={2}>
          <Label htmlFor="gke-exchange-server">MCP server</Label>
          <Select value={server.id} onValueChange={setChosenId}>
            <SelectTrigger id="gke-exchange-server" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {servers.map((option) => (
                <SelectItem key={option.id} value={option.id}>
                  {option.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Text muted small>
            Each access token reaches one server. Exchange once for each server
            the workload calls.
          </Text>
        </Stack>
      )}
      <ol className="border-border divide-border divide-y border">
        <ConsoleRow
          number={1}
          label="Exchange the pod's token at this endpoint"
          fields={[{ label: "Token endpoint", value: tokenEndpoint ?? "" }]}
        />
        <ConsoleRow
          number={2}
          label={
            server !== undefined
              ? `Call ${server.name} with the access token`
              : "Call an MCP server with the access token"
          }
          fields={[
            {
              label: "MCP server URL",
              value: server?.mcpUrl ?? "",
              hint: "The exchange also names it as the resource, so the access token is issued for this server.",
            },
          ]}
          hint={
            server === undefined
              ? "This agent can't reach any MCP servers yet. Go back to MCP selection and choose at least one."
              : undefined
          }
        />
      </ol>
      {placeholders.length > 0 && (
        <PendingValues placeholders={placeholders}>
          {!tokenEndpoint && issuerPicker}
        </PendingValues>
      )}
      <CodeBlock
        value={exchangeScript(tokenEndpoint, server?.mcpUrl)}
        label="Inside the pod"
        language="shell"
      />
    </>
  );
}
