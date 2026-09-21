import type { ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { SettingsSection } from "@/components/page-templates";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { useVerifyIdentityProviderConnectionMutation } from "@gram/client/react-query/verifyIdentityProviderConnection.js";

import { STEP_AFFORDANCES } from "./checklistAffordances";
import { ConnectionChecklist } from "./ConnectionChecklist";
import { ConnectionSetupProgress } from "./ConnectionSetupProgress";
import { ConnectionFacts, ConnectionScopes } from "./OktaConnectionDetails";
import { ClientIdStep, CreateConnectionForm } from "./OktaConnectionForms";
import { RevokeConnectionButton } from "./RevokeConnectionButton";
import {
  CONNECTION_STATUS,
  connectionStep,
  isConnected,
  LAST_ERROR_LABELS,
  VERIFICATION_REASON_LABELS,
  type ConnectionStep,
  type LiveConnection,
} from "../../connectionView";
import {
  inlineError,
  invalidateIdentityProviderQueries,
  SESSION_SECURITY,
} from "../../identityProviderQueries";
import {
  CHECKLIST_SECTION_ID,
  CLIENT_ID_SECTION_ID,
  CONNECTION_SECTION_ID,
  scrollToConnectionCard,
} from "../../tabs";

function VerificationOutcome({
  connection,
}: {
  connection: LiveConnection;
}): JSX.Element | null {
  const reasons = connection.verificationReasons;
  if (!connection.lastError && reasons.length === 0) return null;
  return (
    <div className="flex flex-col gap-3">
      {connection.lastError && (
        <Alert variant="error" alignTop>
          <Text variant="small">{LAST_ERROR_LABELS[connection.lastError]}</Text>
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
                  <Text variant="small">
                    {VERIFICATION_REASON_LABELS[reason]}
                  </Text>
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
  repair: "Re-verify after changing the app’s permissions in Okta.",
  verify: "Verify once the app is set up to use the public key URL (JWKS).",
  submit_client_id: "Verification runs after the client ID is submitted.",
};

const SECTION_LINK = "text-sm underline underline-offset-4";

function NextStepCallout({
  title,
  body,
  children,
}: {
  title: string;
  body: ReactNode;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-3 border p-4">
      <Text className="font-medium">{title}</Text>
      <Text muted small>
        {body}
      </Text>
      {children}
    </div>
  );
}

function ConnectionCard({
  connection,
}: {
  connection: LiveConnection;
}): JSX.Element {
  const queryClient = useQueryClient();
  const step = connectionStep(connection);
  const status = CONNECTION_STATUS[connection.status];
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
  const runVerify = () => {
    if (verify.isPending) return;
    verify.mutate({
      security: SESSION_SECURITY,
      request: {
        verifyIdentityProviderConnectionRequestBody: { id: connection.id },
      },
    });
  };

  return (
    <SettingsSection id={CONNECTION_SECTION_ID}>
      <SettingsSection.Header>
        <div className="flex flex-wrap items-center gap-3">
          <SettingsSection.Title>Connection</SettingsSection.Title>
          <Badge variant="neutral" size="sm">
            Okta
          </Badge>
          <Badge variant={status.variant} size="sm">
            {status.label}
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
            <NextStepCallout
              title="Next: set up your Okta app"
              body="Follow the Okta setup checklist, then paste the app's client ID to verify access."
            >
              <div className="flex flex-wrap gap-3">
                <a className={SECTION_LINK} href={`#${CHECKLIST_SECTION_ID}`}>
                  Open setup checklist
                </a>
                <a className={SECTION_LINK} href={`#${CLIENT_ID_SECTION_ID}`}>
                  Enter client ID
                </a>
              </div>
            </NextStepCallout>
          )}
          {(step === "verify" || step === "repair") && (
            <NextStepCallout
              title={
                step === "repair"
                  ? "Next: fix the connection issues"
                  : "Next: verify your connection"
              }
              body={`Check the app's permissions (scopes), admin role, and public key settings in Okta, then verify access.${
                step === "repair"
                  ? " Application sync is paused until verification passes."
                  : ""
              }`}
            >
              <a className={SECTION_LINK} href={`#${CHECKLIST_SECTION_ID}`}>
                Review setup
              </a>
              <Button disabled={verify.isPending} onClick={runVerify}>
                {verify.isPending ? "Verifying..." : "Verify connection"}
              </Button>
            </NextStepCallout>
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
            <RevokeConnectionButton connection={connection} />
            {step === "connected" && (
              <Button
                variant="secondary"
                disabled={verify.isPending}
                onClick={runVerify}
              >
                {verify.isPending ? "Verifying..." : "Re-verify"}
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
  connection: LiveConnection;
}): JSX.Element {
  return (
    <SettingsSection id={CHECKLIST_SECTION_ID}>
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
            connection={connection}
            affordances={STEP_AFFORDANCES}
          />
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

export function OktaConnectionTab({
  connection,
}: {
  connection: OktaIdentityProviderConnection | undefined;
}): JSX.Element {
  if (!isConnected(connection)) {
    return <CreateConnectionForm />;
  }

  return (
    <div className="flex flex-col gap-10">
      <ConnectionSetupProgress connection={connection} />
      <ChecklistSection connection={connection} />
      {connectionStep(connection) === "submit_client_id" && (
        <ClientIdStep connection={connection} />
      )}
      <ConnectionCard connection={connection} />
    </div>
  );
}
