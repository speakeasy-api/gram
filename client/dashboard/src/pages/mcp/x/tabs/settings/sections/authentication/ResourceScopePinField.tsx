import { Button } from "@/components/ui/Button";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { ScopeMultiSelect } from "@/lib/remote-identity";
import type { RemoteMcpServerScopes } from "@gram/client/models/components/remotemcpserverscopes.js";
import { useId, useMemo } from "react";
import {
  scopePinStatus,
  sharedServerLine,
  unadvertisedPinnedScopes,
  type ResourceScopePin,
} from "./resourceScopePin";

/** "Pinned scopes" for the connected provider: the server's scope pin. */
export function ResourceScopePinField({
  pin,
  scopes,
  connectedClientId,
  issuerScopes,
  serverName,
  disabled,
}: {
  pin: ResourceScopePin;
  scopes: RemoteMcpServerScopes;
  connectedClientId: string | null;
  issuerScopes: string[];
  /** This MCP server's name, kept as written; empty when it has none. */
  serverName: string;
  disabled: boolean;
}): JSX.Element {
  const id = useId();
  const labelId = `${id}-label`;
  const options = useMemo(
    () => [
      ...(scopes.advertisedScopesKnown ? (scopes.advertisedScopes ?? []) : []),
      ...issuerScopes,
    ],
    [scopes, issuerScopes],
  );
  // Flag off, the pin can only be cleared, never added to.
  const readOnly = !scopes.discoveryEnabled;
  const canClear = readOnly && pin.value.length > 0;
  const status = scopePinStatus(scopes, connectedClientId);
  const unadvertised = unadvertisedPinnedScopes(
    scopes,
    pin.value,
    pin.dirty,
    connectedClientId,
  );
  const shared = sharedServerLine(scopes.sharedServerCount);

  return (
    <div className="max-w-md space-y-1.5">
      <Label id={labelId} htmlFor={id} className="block leading-normal">
        Pinned scopes
      </Label>
      <ScopeMultiSelect
        id={id}
        labelId={labelId}
        options={options}
        value={pin.value}
        onValueChange={pin.setValue}
        placeholder={
          scopes.discoveryEnabled ? "Advertised scopes" : "No pinned scopes"
        }
        disabled={disabled || readOnly}
      />
      {canClear ? (
        <Button
          variant="secondary"
          size="sm"
          disabled={disabled}
          onClick={() => pin.setValue([])}
        >
          <Button.Text>Clear pinned scopes</Button.Text>
        </Button>
      ) : null}
      {unadvertised.length > 0 ? (
        <Text small warning className="block">
          {`${serverName || "The MCP server"} does not advertise the following scopes: ${unadvertised.join(", ")}. They will still be requested.`}
        </Text>
      ) : null}
      {status.map((line) => (
        <Text key={line} muted small className="block">
          {line}
        </Text>
      ))}
      {shared ? (
        <Text muted small className="block">
          {shared}
        </Text>
      ) : null}
    </div>
  );
}
