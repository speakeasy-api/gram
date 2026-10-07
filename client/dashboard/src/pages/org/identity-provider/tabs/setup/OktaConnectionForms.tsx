import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/Field";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Input } from "@/components/ui/Input";
import { SettingsSection } from "@/components/page-templates";
import { Text } from "@/components/ui/Text";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { useCreateIdentityProviderConnectionMutation } from "@gram/client/react-query/createIdentityProviderConnection.js";
import { useSetIdentityProviderConnectionSetupMethodMutation } from "@gram/client/react-query/setIdentityProviderConnectionSetupMethod.js";
import { useRecordIdentityProviderConnectionAgentMutation } from "@gram/client/react-query/recordIdentityProviderConnectionAgent.js";
import { useSubmitIdentityProviderConnectionClientIdMutation } from "@gram/client/react-query/submitIdentityProviderConnectionClientId.js";

import { usesClientSecret } from "../../connectionView";
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

type ListingMode = "custom_app" | "oin";

const DEFAULT_LISTING_MODE: ListingMode = "oin";

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
  const invalid = trimmed !== "" && normalizedOrgUrl === undefined;

  const createConnection = (
    listingMode: ListingMode = DEFAULT_LISTING_MODE,
  ) => {
    if (create.isPending || normalizedOrgUrl === undefined) return;
    setOrgUrl(normalizedOrgUrl);
    create.mutate({
      security: SESSION_SECURITY,
      request: {
        createIdentityProviderConnectionRequestBody: {
          orgUrl: normalizedOrgUrl,
          listingMode,
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
              onEnter={() => createConnection()}
              inputMode="url"
              autoCapitalize="none"
              aria-invalid={invalid}
              aria-describedby={
                invalid
                  ? "okta-org-url-help okta-org-url-error"
                  : "okta-org-url-help"
              }
              value={orgUrl}
              onChange={setOrgUrl}
              onBlur={() => {
                if (normalizedOrgUrl !== undefined) setOrgUrl(normalizedOrgUrl);
              }}
              placeholder="https://example.okta.com"
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
              error={invalid}
            />
            <FieldDescription id="okta-org-url-help">
              Use your organization’s address starting with https:// and ending
              in .okta.com, .oktapreview.com, .okta-emea.com, or .okta.mil. You
              can also paste your Okta Admin Console address.
            </FieldDescription>
            {invalid && (
              <p
                id="okta-org-url-error"
                role="alert"
                className="text-destructive text-sm"
              >
                Enter an HTTPS Okta organization or Admin Console URL, such as
                https://example.okta.com.
              </p>
            )}
          </Field>
          <ApiErrorAlert error={create.error} />
          {create.error && (
            <div className="flex flex-col items-start gap-2">
              <Text muted small>
                If your Okta organization does not support client-secret
                authentication, use a custom API Services app instead.
              </Text>
              <Button
                variant="secondary"
                disabled={normalizedOrgUrl === undefined || create.isPending}
                onClick={() => createConnection("custom_app")}
              >
                Continue with a custom API Services app
              </Button>
            </div>
          )}
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            You’ll need Okta admin access to create the app and grant the
            required scopes in Okta.
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <Button
              disabled={normalizedOrgUrl === undefined || create.isPending}
              onClick={() => createConnection()}
            >
              {create.isPending ? "Creating..." : "Continue"}
            </Button>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

/** Picks how the pending connection connects to Okta; switching is allowed until the client ID is submitted. */
export function SetupMethodChooser({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const queryClient = useQueryClient();
  const current: ListingMode = usesClientSecret(connection)
    ? "oin"
    : "custom_app";
  const setMethod = useSetIdentityProviderConnectionSetupMethodMutation({
    onSuccess: () => void invalidateIdentityProviderQueries(queryClient),
    onError: inlineError,
  });

  const switchTo = (next: ListingMode) => {
    if (setMethod.isPending || next === current) return;
    setMethod.mutate({
      security: SESSION_SECURITY,
      request: {
        setIdentityProviderConnectionSetupMethodRequestBody: {
          id: connection.id,
          listingMode: next,
        },
      },
    });
  };

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>
          Choose how to set up the Okta app
        </SettingsSection.Title>
        <SettingsSection.Description>
          The steps below follow your choice. You can switch until you enter the
          app’s client ID.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <RadioCardGroup
            size="sm"
            className="max-w-xl"
            value={current}
            onValueChange={(value) =>
              switchTo(value === "oin" ? "oin" : "custom_app")
            }
            disabled={setMethod.isPending}
            aria-label="Installation method"
          >
            <RadioCard
              value="oin"
              title={
                <span className="flex items-center gap-2">
                  Okta Integration Network (client secret)
                  <Badge variant="success" size="sm">
                    Recommended
                  </Badge>
                </span>
              }
            >
              <Text muted small>
                Install the Speakeasy integration from the Okta catalog and
                paste its client ID and client secret.
              </Text>
            </RadioCard>
            <RadioCard
              value="custom_app"
              title="Custom API Services app (private key)"
            >
              <Text muted small>
                Create an API Services app in Okta that signs in with a private
                key Speakeasy holds.
              </Text>
            </RadioCard>
          </RadioCardGroup>
          <ApiErrorAlert error={setMethod.error} />
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

/** Write-only: the saved secret is never read back, so the field always starts empty. */
export function ClientSecretField({
  id = "okta-client-secret",
  value,
  onChange,
  disabled,
  onEnter,
  replacement = false,
}: {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  disabled: boolean;
  onEnter: () => void;
  replacement?: boolean;
}): JSX.Element {
  return (
    <Field className="max-w-xl">
      <FieldLabel htmlFor={id}>
        {replacement ? "New client secret" : "Client secret"}
      </FieldLabel>
      <Input
        id={id}
        type="password"
        value={value}
        onChange={onChange}
        disabled={disabled}
        onEnter={onEnter}
        autoComplete="new-password"
        spellCheck={false}
        aria-describedby={`${id}-help`}
      />
      <FieldDescription id={`${id}-help`}>
        Copy the client secret from the Speakeasy integration in Okta. It is
        encrypted when saved and never displayed again.
      </FieldDescription>
    </Field>
  );
}

export function ClientIdForm({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const secretMode = usesClientSecret(connection);
  const submit = useSubmitIdentityProviderConnectionClientIdMutation({
    // The request variables can carry the plaintext secret; reset clears them.
    gcTime: 0,
    onSuccess: (updated) => {
      submit.reset();
      setClientSecret("");
      if (updated.status === "verified") {
        toast.success("Connection verified");
      } else {
        toast.warning(
          "Credentials saved. Review the verification results below.",
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
    if (
      !validClientId ||
      (secretMode && !clientSecret.trim()) ||
      submit.isPending
    )
      return;
    submit.mutate({
      security: SESSION_SECURITY,
      request: {
        submitIdentityProviderConnectionClientIDRequestBody: {
          id: connection.id,
          clientId: trimmed,
          ...(secretMode ? { clientSecret } : {}),
        },
      },
    });
  };

  return (
    <section
      id={CLIENT_ID_SECTION_ID}
      aria-label="Submit the Okta client ID"
      className="flex max-w-3xl flex-col gap-4"
    >
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
          Use the {secretMode ? "Speakeasy integration" : "API Services app"}
          &apos;s client ID, not the single sign-on (SSO) app or AI agent ID.
          You can save this ID only once. To change it, revoke this connection
          and connect again.
        </FieldDescription>
        {showClientIdError && (
          <p
            id="okta-client-id-error"
            role="alert"
            className="text-destructive text-sm"
          >
            Enter an Okta client ID starting with 0oa followed by at least 17
            letters or numbers.
          </p>
        )}
      </Field>
      {secretMode && (
        <ClientSecretField
          value={clientSecret}
          onChange={setClientSecret}
          disabled={submit.isPending}
          onEnter={submitClientId}
        />
      )}
      {secretMode && submit.error && (
        <p role="alert" className="text-destructive text-sm">
          Unable to save credentials. Check the Okta app settings and try again.
        </p>
      )}
      {!secretMode && <ApiErrorAlert error={submit.error} />}
      <div>
        <Button
          disabled={
            !validClientId ||
            (secretMode && !clientSecret.trim()) ||
            submit.isPending
          }
          onClick={submitClientId}
        >
          {submit.isPending ? "Verifying..." : "Submit and verify"}
        </Button>
      </div>
    </section>
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
        drives the Okta deep links in the Server connections table below. The
        bound application ID is the 0oa... value in the page URL of the app Okta
        created with the agent, not its Client ID; with it, Speakeasy can check
        that app after each applications sync. Okta does not expose either
        through its API. Leave a field empty to clear it.
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
