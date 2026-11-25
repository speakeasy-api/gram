import { LabeledValue } from "@/components/setup-steps/StepBlocks";
import { FieldError } from "@/components/ui/Field";
import { Stack } from "@/components/ui/Stack";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { AuthRow } from "./AuthRow";

/**
 * The values another platform, such as an identity provider, needs to send
 * clients to this server's shared authorization server. Renders nothing for an
 * issuer that gives each server its own authorization server: those values
 * follow the server's own URL.
 */
export function SharedAuthorizationServerField({
  userSessionIssuer,
  mcpUrl,
}: {
  userSessionIssuer: UserSessionIssuer;
  /** The server's URL, which the shared authorization server takes as its
   * resource. Left out while it is unknown. */
  mcpUrl: string | undefined;
}): JSX.Element | null {
  if (userSessionIssuer.authorizationServerMode !== "shared") {
    return null;
  }

  return (
    <AuthRow
      label="Shared authorization server"
      hint="What another platform needs to sign clients in to this server."
    >
      {userSessionIssuer.issuerUrl ? (
        <Stack gap={4}>
          <LabeledValue
            label="Issuer URL"
            value={userSessionIssuer.issuerUrl}
          />
          <LabeledValue
            label="Token endpoint"
            value={userSessionIssuer.tokenEndpoint}
          />
          {mcpUrl ? <LabeledValue label="Resource" value={mcpUrl} /> : null}
        </Stack>
      ) : (
        <FieldError>
          This issuer's shared authorization server isn't available on this
          deployment, so clients sign in through this server's own. Contact
          Speakeasy support to fix it.
        </FieldError>
      )}
    </AuthRow>
  );
}
