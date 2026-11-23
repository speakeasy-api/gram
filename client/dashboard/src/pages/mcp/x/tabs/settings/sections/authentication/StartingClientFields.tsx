import { Text } from "@/components/ui/Text";
import type { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import { ClientCredentialsFields } from "./IssuerFormFields";

type AuthMethod = CreateRemoteSessionClientFormTokenEndpointAuthMethod | "";

/**
 * The credentials of a client started from a stored one, filled in for review.
 * The stored secret is never fetched: a blank secret asks the server to copy
 * it, which it does only while the client ID is still the stored client's. A
 * client Speakeasy hosts a metadata document for has no credentials to copy,
 * so the new client is published as a document of its own.
 */
export function StartingClientFields({
  starting,
  clientId,
  clientSecret,
  tokenEndpointAuthMethod,
  onClientIdChange,
  onClientSecretChange,
  onTokenEndpointAuthMethodChange,
}: {
  starting: RemoteSessionClient;
  clientId: string;
  clientSecret: string;
  tokenEndpointAuthMethod: AuthMethod;
  onClientIdChange: (value: string) => void;
  onClientSecretChange: (value: string) => void;
  onTokenEndpointAuthMethodChange: (value: AuthMethod) => void;
}): JSX.Element {
  if (starting.clientIdMetadataUri) {
    return (
      <Text muted small className="block">
        Speakeasy publishes this client itself, so the new client gets its own
        client ID and metadata document. There is no secret to copy.
      </Text>
    );
  }
  const sameRegistration = clientId.trim() === starting.clientId;
  return (
    <div className="space-y-2">
      <ClientCredentialsFields
        showHeading={false}
        clientId={clientId}
        clientSecret={clientSecret}
        tokenEndpointAuthMethod={tokenEndpointAuthMethod}
        clientSecretLabel="Client Secret"
        clientSecretPlaceholder={
          sameRegistration
            ? "Leave blank to copy the stored secret"
            : "Secret for this client ID"
        }
        // The same registration lists the stored client's redirect URI; a
        // different one registers the URI of a client created now.
        callbackURL={
          sameRegistration ? (starting.callbackUrl ?? null) : undefined
        }
        onClientIdChange={onClientIdChange}
        onClientSecretChange={onClientSecretChange}
        onTokenEndpointAuthMethodChange={onTokenEndpointAuthMethodChange}
      />
      <Text muted small className="block">
        {sameRegistration
          ? "The stored secret is copied on the server when you save and is never shown here. Enter a secret only to use a different one."
          : "A different client ID is a different registration, so no secret is copied. Enter the secret for this client ID."}
      </Text>
    </div>
  );
}
