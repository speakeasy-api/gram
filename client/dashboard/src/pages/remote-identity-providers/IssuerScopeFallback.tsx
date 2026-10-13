import { Text } from "@/components/ui/Text";

/** Explain inherited scopes without presenting them as a saved client selection. */
export function IssuerScopeFallback({
  scopes,
}: {
  scopes?: string[] | null;
}): JSX.Element | null {
  if (!scopes?.length) return null;
  return (
    <Text small muted>
      The identity provider's scope fallback is {scopes.join(", ")}. With MCP
      scope discovery, a client selection takes precedence; an empty selection
      uses the MCP server's scopes before this fallback. Without MCP scope
      discovery, the provider's fallback takes precedence over the client.
      Change it in the identity provider's Settings.
    </Text>
  );
}
