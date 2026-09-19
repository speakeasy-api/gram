import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { SettingsSection } from "@/components/page-templates";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { useRecordIdentityProviderConnectionAgentMutation } from "@gram/client/react-query/recordIdentityProviderConnectionAgent.js";
import { useRevokeIdentityProviderConnectionMutation } from "@gram/client/react-query/revokeIdentityProviderConnection.js";
import { useSubmitIdentityProviderConnectionClientIdMutation } from "@gram/client/react-query/submitIdentityProviderConnectionClientId.js";

import {
  inlineError,
  invalidateIdentityProviderQueries,
  scrollToConnectionCard,
  SESSION_SECURITY,
} from "./identityProviderQueries";

export const AGENT_SECTION_ID = "agent";

function orgHostOf(orgUrl: string): string {
  try {
    return new URL(orgUrl).host.toLowerCase();
  } catch {
    return orgUrl.replace(/^https?:\/\//i, "").toLowerCase();
  }
}

export function ClientIdStep({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [clientId, setClientId] = useState("");
  const submit = useSubmitIdentityProviderConnectionClientIdMutation({
    onSuccess: (updated) => {
      if (updated.status === "verified") {
        toast.success("Connection verified");
      } else {
        toast.warning(
          "Client ID saved. Review the verification results below.",
        );
      }
      void invalidateIdentityProviderQueries(queryClient).then(
        scrollToConnectionCard,
      );
    },
    onError: inlineError,
  });
  const trimmed = clientId.trim();
  const validClientId = /^0oa[A-Za-z0-9]{17,}$/.test(trimmed);
  const showClientIdError = trimmed !== "" && !validClientId;
  const submitClientId = () => {
    if (!validClientId || submit.isPending) return;
    submit.mutate({
      security: SESSION_SECURITY,
      request: {
        submitIdentityProviderConnectionClientIDRequestBody: {
          id: connection.id,
          clientId: trimmed,
        },
      },
    });
  };

  return (
    <SettingsSection id="client-id">
      <SettingsSection.Header>
        <SettingsSection.Title>Paste the client ID</SettingsSection.Title>
        <SettingsSection.Description>
          Complete the Connect section of the checklist, then paste the client
          ID of the Okta API Services app. Speakeasy checks the connection right
          away. You can save this ID only once. To change it, revoke this
          connection and connect again.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field className="max-w-xl">
            <FieldLabel htmlFor="okta-client-id">Client ID</FieldLabel>
            <Input
              id="okta-client-id"
              disabled={submit.isPending}
              onEnter={submitClientId}
              aria-invalid={showClientIdError}
              aria-describedby={`okta-client-id-help${showClientIdError ? " okta-client-id-error" : ""}`}
              error={showClientIdError}
              value={clientId}
              onChange={setClientId}
              placeholder="0oa..."
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
            />
            <FieldDescription id="okta-client-id-help">
              Found under Applications, in the API Services app&apos;s General
              tab. Use this app&apos;s client ID, not the single sign-on (SSO)
              app or AI agent ID.
            </FieldDescription>
            {showClientIdError && (
              <p
                id="okta-client-id-error"
                role="alert"
                className="text-destructive text-sm"
              >
                Enter an Okta client ID starting with 0oa followed by at least
                17 letters or numbers.
              </p>
            )}
          </Field>
          <ApiErrorAlert error={submit.error} />
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Speakeasy checks that it can connect securely to Okta and read
            applications, users, and groups.
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <Button
              disabled={!validClientId || submit.isPending}
              onClick={submitClientId}
            >
              {submit.isPending ? "Verifying..." : "Submit and verify"}
            </Button>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

export function AgentSetupForm({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [agentId, setAgentId] = useState(connection.agentId ?? "");
  const [agentAppId, setAgentAppId] = useState(connection.agentAppId ?? "");
  const record = useRecordIdentityProviderConnectionAgentMutation({
    onSuccess: () => {
      toast.success("Agent details saved");
      void invalidateIdentityProviderQueries(queryClient);
    },
    onError: inlineError,
  });
  const dirty =
    agentId.trim() !== (connection.agentId ?? "") ||
    agentAppId.trim() !== (connection.agentAppId ?? "");

  const saveAgent = () => {
    if (!dirty || record.isPending) return;
    record.mutate({
      security: SESSION_SECURITY,
      request: {
        recordIdentityProviderConnectionAgentRequestBody: {
          id: connection.id,
          agentId: agentId.trim(),
          agentAppId: agentAppId.trim(),
        },
      },
    });
  };

  return (
    <section
      id={AGENT_SECTION_ID}
      aria-label="Save Okta AI agent"
      className="flex max-w-3xl flex-col gap-4"
    >
      <div className="grid max-w-3xl grid-cols-1 gap-4 md:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="okta-agent-id">Agent ID</FieldLabel>
          <Input
            id="okta-agent-id"
            disabled={record.isPending}
            onEnter={saveAgent}
            aria-describedby="okta-agent-help"
            value={agentId}
            onChange={setAgentId}
            className="font-mono"
            autoComplete="off"
            spellCheck={false}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="okta-agent-app-id">
            Bound application ID (optional)
          </FieldLabel>
          <Input
            id="okta-agent-app-id"
            disabled={record.isPending}
            onEnter={saveAgent}
            aria-describedby="okta-agent-help"
            value={agentAppId}
            onChange={setAgentAppId}
            className="font-mono"
            autoComplete="off"
            spellCheck={false}
          />
        </Field>
      </div>
      <ApiErrorAlert error={record.error} />
      <FieldDescription id="okta-agent-help">
        The agent ID is the wlp... value in the Okta agent page URL, and it
        drives the deep links on the Cross App Access tab. The bound application
        ID is the OIDC app Okta created with the agent, kept for reference. Okta
        does not expose either through its API. Leave a field empty to clear it.
      </FieldDescription>
      <a
        href="https://help.okta.com/oie/en-us/content/topics/ai-agents/ai-agent-add-manually.htm"
        target="_blank"
        rel="noopener noreferrer"
        className="w-fit text-sm underline underline-offset-4"
      >
        Okta AI agent registration guide
      </a>
      <div>
        <Button disabled={!dirty || record.isPending} onClick={saveAgent}>
          {record.isPending ? "Saving..." : "Save agent"}
        </Button>
      </div>
    </section>
  );
}

export function RevokeConnectionButton({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState("");
  const revoke = useRevokeIdentityProviderConnectionMutation({
    onSuccess: () => {
      toast.success("Okta connection revoked");
      setOpen(false);
      void invalidateIdentityProviderQueries(queryClient);
    },
    onError: inlineError,
  });
  const orgHost = orgHostOf(connection.orgUrl);
  const confirmed = typed.trim().toLowerCase() === orgHost;
  const close = () => {
    if (revoke.isPending) return;
    setOpen(false);
    setTyped("");
    revoke.reset();
  };

  return (
    <>
      <Button variant="destructive-secondary" onClick={() => setOpen(true)}>
        Revoke
      </Button>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (next) setOpen(true);
          else close();
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Revoke the Okta connection</Dialog.Title>
            <Dialog.Description>
              Speakeasy stops connecting to Okta and removes this
              connection&apos;s public keys. Application sync stops, and
              Speakeasy no longer tracks Cross App Access setup. You can
              reconnect afterwards with a new client ID.
            </Dialog.Description>
          </Dialog.Header>
          <div className="space-y-3 py-2">
            <Field>
              <FieldLabel htmlFor="okta-revoke-confirm">
                Type <span className="font-mono">{orgHost}</span> to confirm
              </FieldLabel>
              <Input
                id="okta-revoke-confirm"
                value={typed}
                onChange={setTyped}
                className="font-mono"
                autoComplete="off"
                spellCheck={false}
              />
            </Field>
            <ApiErrorAlert error={revoke.error} />
          </div>
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={close}
              disabled={revoke.isPending}
            >
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              disabled={!confirmed || revoke.isPending}
              onClick={() =>
                revoke.mutate({
                  security: SESSION_SECURITY,
                  request: {
                    revokeIdentityProviderConnectionRequestBody: {
                      id: connection.id,
                    },
                  },
                })
              }
            >
              {revoke.isPending ? "Revoking..." : "Revoke connection"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
