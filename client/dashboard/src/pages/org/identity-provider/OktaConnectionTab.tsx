import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { SettingsSection } from "@/components/page-templates";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { useCreateIdentityProviderConnectionMutation } from "@gram/client/react-query/createIdentityProviderConnection.js";
import { useVerifyIdentityProviderConnectionMutation } from "@gram/client/react-query/verifyIdentityProviderConnection.js";

import { ConnectionNextSteps } from "./ConnectionNextSteps";
import { ConnectionChecklist } from "./ConnectionChecklist";
import { ConnectionFacts, ConnectionScopes } from "./OktaConnectionDetails";
import { ClientIdStep, RevokeConnectionButton } from "./OktaConnectionSteps";
import {
  connectionStatusLabel,
  connectionStatusVariant,
  connectionStep,
  lastErrorLabel,
  normalizeOktaOrgUrl,
  verificationReasonLabel,
  type ConnectionStep,
} from "./connectionView";
import {
  CONNECTION_SECTION_ID,
  inlineError,
  invalidateIdentityProviderQueries,
  scrollToConnectionCard,
  SESSION_SECURITY,
} from "./identityProviderQueries";
import type { ConnectionTabProps } from "./IdentityProviderArea";

import { ConnectionSetupProgress } from "./ConnectionSetupProgress";

function CreateConnectionForm(): JSX.Element {
  const queryClient = useQueryClient();
  const [orgUrl, setOrgUrl] = useState("");
  const create = useCreateIdentityProviderConnectionMutation({
    onSuccess: () => {
      toast.success("Okta connection created");
      void invalidateIdentityProviderQueries(queryClient);
    },
    onError: inlineError,
  });
  const trimmed = orgUrl.trim();
  const normalizedOrgUrl = normalizeOktaOrgUrl(trimmed);

  const createConnection = () => {
    if (create.isPending || normalizedOrgUrl === undefined) return;
    create.mutate({
      security: SESSION_SECURITY,
      request: {
        createIdentityProviderConnectionRequestBody: {
          orgUrl: normalizedOrgUrl,
          listingMode: "custom_app",
        },
      },
    });
  };

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>
          Connect your Okta organization
        </SettingsSection.Title>
        <SettingsSection.Description>
          Enter your Okta organization URL to get started. We’ll guide you
          through setting up the Okta app next. This does not change anything in
          Okta.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field className="max-w-xl">
            <FieldLabel htmlFor="okta-org-url">
              Okta organization URL
            </FieldLabel>
            <Input
              id="okta-org-url"
              disabled={create.isPending}
              onEnter={createConnection}
              inputMode="url"
              autoCapitalize="none"
              aria-invalid={trimmed !== "" && normalizedOrgUrl === undefined}
              aria-describedby="okta-org-url-help okta-org-url-error"
              value={orgUrl}
              onChange={setOrgUrl}
              placeholder="https://example.okta.com"
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
              error={trimmed !== "" && normalizedOrgUrl === undefined}
            />
            <FieldDescription id="okta-org-url-help">
              Use your organization’s address starting with https:// and ending
              in .okta.com, .oktapreview.com, .okta-emea.com, or .okta.mil. Do
              not include a page address after the domain.
            </FieldDescription>
            {trimmed !== "" && normalizedOrgUrl === undefined && (
              <p
                id="okta-org-url-error"
                role="alert"
                className="text-destructive text-sm"
              >
                Enter an HTTPS Okta organization URL, such as
                https://example.okta.com, with nothing after the domain.
              </p>
            )}
          </Field>
          <ApiErrorAlert error={create.error} />
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            You’ll need Okta admin access to create the app and grant the
            required permissions (called scopes in Okta).
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <Button
              disabled={normalizedOrgUrl === undefined || create.isPending}
              onClick={createConnection}
            >
              {create.isPending ? "Creating..." : "Create connection"}
            </Button>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function VerificationOutcome({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element | null {
  const reasons = connection.verificationReasons;
  if (!connection.lastError && reasons.length === 0) return null;
  return (
    <div className="flex flex-col gap-3">
      {connection.lastError && (
        <Alert variant="error" alignTop>
          <Text variant="small">{lastErrorLabel(connection.lastError)}</Text>
        </Alert>
      )}
      {reasons.length > 0 && (
        <Alert variant="warning" alignTop>
          <div className="flex flex-col gap-1">
            <Text variant="small" className="font-medium">
              The connection needs attention
            </Text>
            <ul className="list-disc space-y-0.5 pl-4">
              {reasons.map((reason) => (
                <li key={reason}>
                  <Text variant="small">{verificationReasonLabel(reason)}</Text>
                </li>
              ))}
            </ul>
            {connection.missingScopes.length > 0 && (
              <Text variant="small">
                Grant {connection.missingScopes.join(", ")} to the app in Okta,
                then re-verify. Application sync stays paused until verification
                passes.
              </Text>
            )}
          </div>
        </Alert>
      )}
    </div>
  );
}

const FOOTER_HINTS: Record<ConnectionStep, string> = {
  connected: "Re-verify after changing the app’s permissions in Okta.",
  verify: "Verify once the app is set up to use the public key URL (JWKS).",
  submit_client_id: "Verification runs after the client ID is submitted.",
  revoked: "This connection was revoked. Create a new one to reconnect Okta.",
};

function ConnectionCard({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const queryClient = useQueryClient();
  const step = connectionStep(connection);
  const verify = useVerifyIdentityProviderConnectionMutation({
    onSuccess: (updated) => {
      if (updated.status === "verified") {
        toast.success("Connection verified");
      } else {
        toast.warning(
          "Verification needs attention. Review the results below.",
        );
      }
      void invalidateIdentityProviderQueries(queryClient).then(
        scrollToConnectionCard,
      );
    },
    onError: inlineError,
  });
  const needsRepair = connection.status === "degraded";
  const verifyLabel = step === "connected" ? "Re-verify" : "Verify";

  return (
    <SettingsSection id={CONNECTION_SECTION_ID}>
      <SettingsSection.Header>
        <div className="flex flex-wrap items-center gap-3">
          <SettingsSection.Title>Connection</SettingsSection.Title>
          <Badge variant="neutral" size="sm">
            Okta
          </Badge>
          <Badge variant={connectionStatusVariant(connection.status)} size="sm">
            {connectionStatusLabel(connection.status)}
          </Badge>
        </div>
        <SettingsSection.Description>
          Speakeasy uses this API Services app to connect to Okta. Okta reads
          public keys from the public key URL (JWKS) below to check that
          requests come from Speakeasy. Private keys stay with Speakeasy.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {step === "submit_client_id" && (
            <div className="flex flex-col gap-3 border p-4">
              <Text className="font-medium">Next: set up your Okta app</Text>
              <Text muted small>
                Follow the Okta setup checklist, then paste the app&apos;s
                client ID to verify access.
              </Text>
              <div className="flex flex-wrap gap-3">
                <a
                  className="text-sm underline underline-offset-4"
                  href="#checklist"
                >
                  Open setup checklist
                </a>
                <a
                  className="text-sm underline underline-offset-4"
                  href="#client-id"
                >
                  Enter client ID
                </a>
              </div>
            </div>
          )}
          {(step === "verify" || needsRepair) && (
            <div className="flex flex-col gap-3 border p-4">
              <Text className="font-medium">
                {needsRepair
                  ? "Next: fix the connection issues"
                  : "Next: verify your connection"}
              </Text>
              <Text muted small>
                Check the app&apos;s permissions (scopes), admin role, and
                public key settings in Okta, then verify access.
                {needsRepair &&
                  " Application sync is paused until verification passes."}
              </Text>
              <a
                className="text-sm underline underline-offset-4"
                href="#checklist"
              >
                Review setup
              </a>
              <Button
                disabled={verify.isPending}
                onClick={() => {
                  if (verify.isPending) return;
                  verify.mutate({
                    security: SESSION_SECURITY,
                    request: {
                      verifyIdentityProviderConnectionRequestBody: {
                        id: connection.id,
                      },
                    },
                  });
                }}
              >
                {verify.isPending ? "Verifying..." : "Verify connection"}
              </Button>
            </div>
          )}
          <ConnectionFacts connection={connection} />
          <VerificationOutcome connection={connection} />
          <ApiErrorAlert error={verify.error} />
        </SettingsSection.Body>
        <SettingsSection.Body className="border-t">
          <ConnectionScopes connection={connection} />
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            {FOOTER_HINTS[step]}
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            {step !== "revoked" && (
              <RevokeConnectionButton connection={connection} />
            )}
            {step === "connected" && !needsRepair && (
              <Button
                variant="secondary"
                disabled={verify.isPending}
                onClick={() =>
                  verify.mutate({
                    security: SESSION_SECURITY,
                    request: {
                      verifyIdentityProviderConnectionRequestBody: {
                        id: connection.id,
                      },
                    },
                  })
                }
              >
                {verify.isPending ? "Verifying..." : verifyLabel}
              </Button>
            )}
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function ChecklistSection({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  return (
    <SettingsSection id="checklist">
      <SettingsSection.Header>
        <SettingsSection.Title>Okta setup checklist</SettingsSection.Title>
        <SettingsSection.Description>
          Follow these steps in the Okta Admin Console. Speakeasy marks steps
          complete when it has evidence from the connection check. Review any
          steps marked Not checked yourself.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <ConnectionChecklist
            key={connection.status}
            connection={connection}
          />
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

export function OktaConnectionTab({
  connection,
}: Pick<ConnectionTabProps, "connection">): JSX.Element {
  if (!connection || connection.status === "revoked") {
    return <CreateConnectionForm />;
  }

  const step = connectionStep(connection);
  return (
    <div className="flex flex-col gap-10">
      <ConnectionSetupProgress connection={connection} />
      <ConnectionNextSteps connection={connection} />
      <ConnectionCard connection={connection} />
      <ChecklistSection connection={connection} />
      {step === "submit_client_id" && <ClientIdStep connection={connection} />}
    </div>
  );
}
