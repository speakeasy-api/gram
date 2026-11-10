import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useState } from "react";
import { remoteSessionClientDisplayName } from "../../clientDisplay";
import { DeleteClientDialog, RotateClientDialog } from "../../clientDialogs";
import { DuplicateClientFlow } from "../../DuplicateClient";

// ClientLifecycleSections holds the Registration, Duplicate, and Danger Zone
// sections below the client card.
export function ClientLifecycleSections({
  client,
  issuer,
  issuerId,
}: {
  client: RemoteSessionClient;
  issuer: RemoteSessionIssuer | undefined;
  issuerId: string;
}): JSX.Element {
  const routes = useRoutes();
  const [showDelete, setShowDelete] = useState(false);
  const [showRotate, setShowRotate] = useState(false);
  const [showDuplicate, setShowDuplicate] = useState(false);
  // A rotation re-registers the client at the issuer's published registration
  // endpoint, so the issuer decides whether one is possible at all.
  const issuerRegistrationEndpoint = issuer?.registrationEndpoint ?? null;

  return (
    <>
      <Card>
        <Card.Header>Registration</Card.Header>
        <Card.Content>
          <div className="flex flex-col gap-3">
            <RegistrationStatus
              client={client}
              issuerRegistrationEndpoint={issuerRegistrationEndpoint}
            />
            {isRotatableRegistration(client) && issuerRegistrationEndpoint && (
              <div>
                <RequireScope scope="org:admin" level="component">
                  <Button
                    variant="secondary"
                    onClick={() => setShowRotate(true)}
                  >
                    <Button.Text>Rotate client</Button.Text>
                  </Button>
                </RequireScope>
              </div>
            )}
          </div>
        </Card.Content>
      </Card>

      <RequireScope scope="org:admin" level="component">
        <Card>
          <Card.Header>Duplicate</Card.Header>
          <Card.Content>
            <div className="flex flex-col gap-3">
              <Text small muted>
                Create a new client with this client&apos;s settings, to change
                some of them without affecting the MCP servers that use this
                one.
              </Text>
              <div>
                <Button
                  variant="secondary"
                  onClick={() => setShowDuplicate(true)}
                >
                  <Button.Text>Duplicate client</Button.Text>
                </Button>
              </div>
            </div>
          </Card.Content>
        </Card>
      </RequireScope>

      <Card className="border-destructive/30">
        <Card.Header className="text-destructive">Danger zone</Card.Header>
        <Card.Content>
          <div className="flex flex-col gap-3">
            <Text small muted>
              Deleting this client is permanent and revokes all of its sessions.
            </Text>
            <div>
              <RequireScope scope="org:admin" level="component">
                <Button
                  variant="destructive-primary"
                  onClick={() => setShowDelete(true)}
                >
                  <Button.Text>Delete client</Button.Text>
                </Button>
              </RequireScope>
            </div>
          </div>
        </Card.Content>
      </Card>

      {showRotate && (
        <RotateClientDialog
          clientId={client.id}
          clientLabel={remoteSessionClientDisplayName(client)}
          onClose={() => setShowRotate(false)}
        />
      )}

      {showDuplicate && (
        <DuplicateClientFlow
          client={client}
          issuer={issuer}
          issuerId={issuerId}
          onClose={() => setShowDuplicate(false)}
        />
      )}

      {showDelete && (
        <DeleteClientDialog
          clientId={client.id}
          clientLabel={remoteSessionClientDisplayName(client)}
          onClose={() => setShowDelete(false)}
          onDeleted={() =>
            routes.remoteIdentityProviders.issuerDetail.goTo(issuerId)
          }
        />
      )}
    </>
  );
}

// RegistrationStatus explains what a rotation would do for this client: whether
// the identity provider publishes a registration endpoint Speakeasy can re-register
// it at, whether the provider has already stopped recognizing it, and when its
// secret expires.
function RegistrationStatus({
  client,
  issuerRegistrationEndpoint,
}: {
  client: RemoteSessionClient;
  issuerRegistrationEndpoint: string | null;
}): JSX.Element {
  if (!isRotatableRegistration(client)) {
    return (
      <Text small muted>
        {client.clientIdMetadataUri
          ? "This client uses a client ID metadata document hosted by Speakeasy. It is never registered with the authorization server, so it cannot expire and has nothing to rotate."
          : "This client authenticates with a signed assertion bound to a key set, which dynamic registration cannot reproduce. Manage its key set instead of rotating it."}
      </Text>
    );
  }
  if (client.upstreamRejectedAt) {
    return (
      <Text small className="text-destructive">
        The authorization server stopped recognizing this client on{" "}
        {client.upstreamRejectedAt.toLocaleString()}.{" "}
        {issuerRegistrationEndpoint
          ? "Rotate it to register a replacement; users reconnect once afterwards."
          : "The authorization server publishes no registration endpoint, so replace the client ID and secret by hand."}
      </Text>
    );
  }
  const expiryNote = client.clientSecretExpiresAt
    ? ` The secret expires ${client.clientSecretExpiresAt.toLocaleString()}.`
    : "";
  if (issuerRegistrationEndpoint) {
    return (
      <Text small muted>
        Speakeasy re-registers this client at {issuerRegistrationEndpoint} if
        the authorization server stops recognizing it or its secret expires;
        rotate now to replace it ahead of time.{expiryNote}
      </Text>
    );
  }
  return (
    <Text small muted>
      The authorization server publishes no registration endpoint, so Speakeasy
      cannot re-register this client; replace its credentials by hand if the
      authorization server stops recognizing them.{expiryNote}
    </Text>
  );
}

// isRotatableRegistration mirrors the server's rule: a rotation replaces a
// dynamically registered client_id and secret, which a CIMD client (its
// client_id is a document URL) and a private_key_jwt client (bound to a key
// set) do not have.
function isRotatableRegistration(client: RemoteSessionClient): boolean {
  // The SDK enum does not list private_key_jwt until it becomes selectable,
  // but the server already stores and refuses to rotate it.
  const authMethod: string | undefined = client.tokenEndpointAuthMethod;
  return !client.clientIdMetadataUri && authMethod !== "private_key_jwt";
}
