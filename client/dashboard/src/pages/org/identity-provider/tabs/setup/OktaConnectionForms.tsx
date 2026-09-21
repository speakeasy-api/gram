import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { Button } from "@/components/ui/Button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { SettingsSection } from "@/components/page-templates";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { useCreateIdentityProviderConnectionMutation } from "@gram/client/react-query/createIdentityProviderConnection.js";
import { useRecordIdentityProviderConnectionAgentMutation } from "@gram/client/react-query/recordIdentityProviderConnectionAgent.js";
import { useSubmitIdentityProviderConnectionClientIdMutation } from "@gram/client/react-query/submitIdentityProviderConnectionClientId.js";

import {
  inlineError,
  invalidateIdentityProviderQueries,
  SESSION_SECURITY,
} from "../../identityProviderQueries";
import { normalizeOktaOrgUrl } from "../../oktaConsoleLinks";
import {
  AGENT_SECTION_ID,
  CLIENT_ID_SECTION_ID,
  scrollToConnectionCard,
} from "../../tabs";

export function CreateConnectionForm(): JSX.Element {
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
              aria-describedby={
                trimmed !== "" && normalizedOrgUrl === undefined
                  ? "okta-org-url-help okta-org-url-error"
                  : "okta-org-url-help"
              }
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
    <SettingsSection id={CLIENT_ID_SECTION_ID}>
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
        ID is the Client ID of the app Okta created with the agent; with it,
        Speakeasy can check that app after each applications sync. Okta does not
        expose either through its API. Leave a field empty to clear it.
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
