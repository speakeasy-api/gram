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
import { shellQuote } from "@/lib/shell";
import { useState, type JSX, type ReactNode } from "react";
import { CodeBlock } from "./CodeBlock";
import { ConsoleRow } from "./ConsoleRow";
import { isCredentialSafe } from "./gateway";
import { PendingValues } from "./SetupFields";
import type { SetupServer } from "./setupInstructions";

/**
 * The exchange, run inside the pod each time it needs a Speakeasy token. It
 * names no resource: the shared authorization server then issues one access
 * token for every MCP server of its issuer, and the agent's policy decides
 * which of them answer. Naming one would bind the token to that server, and
 * the pod's identity token is accepted once, so a second server's exchange
 * would be refused until Kubernetes rotates it.
 */
function exchangeScript(tokenEndpoint?: string, mcpUrl?: string) {
  return [
    `TOKEN_ENDPOINT=${shellQuote(tokenEndpoint || "SPEAKEASY_TOKEN_ENDPOINT")}`,
    `MCP_URL=${shellQuote(mcpUrl || "MCP_SERVER_URL")}`,
    "",
    "# Read the file each time: Kubernetes replaces the token before it expires.",
    'response=$(curl -sS "$TOKEN_ENDPOINT" \\',
    '  --data-urlencode "grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer" \\',
    '  --data-urlencode "assertion=$(cat "$SPEAKEASY_TOKEN_FILE")")',
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
 * The exchange step's values: where to exchange the pod's token, and a server
 * to call with the access token. One access token reaches every server that
 * accepts the selected issuer, so the reader picks which one the example
 * calls. Servers that refuse it are left out of `servers` and only counted.
 */
export function ExchangeValues({
  tokenEndpoint,
  servers,
  serversLeftOut = 0,
  issuerPicker,
}: {
  tokenEndpoint?: string;
  servers: SetupServer[];
  serversLeftOut?: number;
  issuerPicker: ReactNode;
}): JSX.Element {
  const [chosenId, setChosenId] = useState("");
  // The call carries the access token as a bearer credential, so a server
  // only reachable over plaintext HTTP is never offered as a target.
  const reachable = servers.filter((s) => isCredentialSafe(s.mcpUrl));
  const server = reachable.find((s) => s.id === chosenId) ?? reachable[0];
  const placeholders = [
    ...(tokenEndpoint ? [] : ["SPEAKEASY_TOKEN_ENDPOINT"]),
    ...(server ? [] : ["MCP_SERVER_URL"]),
  ];
  return (
    <>
      <p className="text-sm">
        Before calling MCP servers, the workload exchanges its token for a
        Speakeasy access token.{" "}
        {serversLeftOut > 0
          ? "One access token works at every server that signs in through the selected user session issuer."
          : "One access token works at every server this agent may reach."}
      </p>
      {serversLeftOut > 0 && tokenEndpoint && issuerPicker}
      {reachable.length > 1 && server !== undefined && (
        <Stack gap={2}>
          <Label htmlFor="gke-exchange-server">MCP server</Label>
          <Select value={server.id} onValueChange={setChosenId}>
            <SelectTrigger id="gke-exchange-server" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {reachable.map((option) => (
                <SelectItem key={option.id} value={option.id}>
                  {option.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Text muted small>
            The example below calls this server. The same access token works at
            the others.
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
            },
          ]}
          hint={
            server !== undefined
              ? undefined
              : servers.length > 0
                ? "This agent's servers are only served over plaintext HTTP, so their URLs are withheld: the access token would cross the network unencrypted. Serve them over HTTPS and reload."
                : serversLeftOut > 0
                  ? "None of this agent's servers accept a token from the selected user session issuer. The user session issuer section on this step names what each one needs."
                  : "This agent can't reach any MCP servers yet. Go back to MCP Selection and choose at least one."
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
