import { AppWindow, Monitor } from "lucide-react";
import { useNewRemoteSessionClientCallbackUrl } from "@gram/client/react-query/newRemoteSessionClientCallbackUrl.js";
import { useSetup } from "./setup-context";
import { oktaOrg, oktaScopeGuide } from "./okta";
import {
  AppChoice,
  CallbackUrlStatus,
  Choices,
  CopyValue,
  ExternalAction,
  InProvider,
  Section,
  SetupField,
} from "./shared";

export function OktaCredentialInstructions(): JSX.Element | null {
  const { s } = useSetup();
  if (s.saved) return null;
  return (
    <InProvider
      actions={[
        <>
          Open your app’s <strong>General</strong> tab.
        </>,
        <>
          Copy <strong>Client ID</strong> into the Client ID field below.
        </>,
        ...(s.values.oktaAppType === "web"
          ? [
              <>
                Copy <strong>Client Secret</strong> from{" "}
                <strong>Client Credentials</strong> into the Client Secret field
                below.
              </>,
            ]
          : []),
      ]}
    />
  );
}

function OktaPermissions(): JSX.Element {
  const { s } = useSetup();
  return (
    <>
      <Section title="Grant API scopes">
        <InProvider
          actions={[
            <>
              As a Super Administrator, open the app’s{" "}
              <strong>Okta API Scopes</strong> tab.
            </>,
            <>
              Click <strong>Grant</strong> for each API scope needed by the
              tools you want to use.
            </>,
          ]}
        />
        <a
          href={oktaScopeGuide}
          target="_blank"
          rel="noopener noreferrer"
          className="mt-3 inline-block text-link-primary underline"
        >
          Find the API scopes for your tools
        </a>
      </Section>
      <Section title="Configure app authentication">
        <InProvider
          actions={[
            <>
              Open the app’s <strong>General</strong> tab.
            </>,
            <>
              Click <strong>Edit</strong> in <strong>Client Credentials</strong>
              .
            </>,
            <>
              Set <strong>Client authentication</strong> to{" "}
              <strong>
                {s.values.oktaAppType === "web" ? "Client secret" : "None"}
              </strong>
              .
            </>,
            ...(s.values.oktaAppType === "web"
              ? [
                  <>
                    Select <strong>Proof Key for Code Exchange (PKCE)</strong>.
                  </>,
                ]
              : []),
            <>Save the changes.</>,
          ]}
        />
      </Section>
      {s.values.appChoice === "existing" && (
        <Section title="Assign people">
          <InProvider
            actions={[
              <>
                Open the <strong>Assignments</strong> tab.
              </>,
              <>Assign the people or groups who should use this app.</>,
            ]}
          />
          <p className="mt-3 text-sm text-muted-foreground">
            People keep their existing Okta permissions.
          </p>
        </Section>
      )}
    </>
  );
}

export function OktaTask(): JSX.Element {
  const { s, live } = useSetup();
  const callback = useNewRemoteSessionClientCallbackUrl(undefined, undefined, {
    throwOnError: false,
  });
  const org = oktaOrg(s.values.oktaOrg);
  const redirect = s.saved ? live.callbackUrl : callback.data?.callbackUrl;
  if (s.step === "enable")
    return (
      <>
        <InProvider
          actions={[
            <>
              In the Okta Admin Console, open{" "}
              <strong>Settings → Features</strong>.
            </>,
            <>
              Enable the Okta Managed MCP Server features you need:
              <ul className="my-2 list-disc pl-6">
                <li>
                  <strong>Core Identity</strong> — users, groups, apps,
                  policies, devices, and logs.
                </li>
                <li>
                  <strong>Identity Governance</strong> — access requests, access
                  reviews, entitlements, and resource ownership.
                </li>
              </ul>
            </>,
          ]}
        />
        <p className="mt-3 text-sm text-muted-foreground">
          An Okta Managed MCP Server requires an eligible subscription.
        </p>
      </>
    );
  if (s.step === "organization")
    return (
      <>
        <SetupField
          field="oktaOrg"
          label="Okta organization URL"
          placeholder="https://example.okta.com"
        />
        {org && <CopyValue label="MCP server URL" value={`${org}/mcp`} />}
      </>
    );
  if (s.step === "permissions") return <OktaPermissions />;
  return (
    <>
      <AppChoice />
      <Choices
        field="oktaAppType"
        label="Application type"
        options={[
          {
            value: "web",
            title: "Web app",
            description:
              "For server-side apps and chatbots. Recommended for Speakeasy.",
            icon: AppWindow,
          },
          {
            value: "native",
            title: "Native app",
            description: "For desktop MCP clients such as VS Code.",
            icon: Monitor,
          },
        ]}
      />
      <ExternalAction
        title={
          s.values.appChoice === "existing"
            ? "Open your Okta app"
            : "Create an Okta app"
        }
        label="Open Okta"
        href={org ?? undefined}
        disabled={!org}
        disabledReason="Enter a valid Okta organization URL in the Okta organization step to enable this link."
      />
      <InProvider
        actions={[
          <>
            In the Admin Console, open{" "}
            <strong>Applications and resources → Applications</strong>.
          </>,
          ...(s.values.appChoice === "existing"
            ? [
                <>
                  Open the{" "}
                  <strong>
                    {s.values.oktaAppType === "web"
                      ? "Web Application"
                      : "Native Application"}
                  </strong>{" "}
                  you want to use.
                </>,
                <>
                  Open the app’s <strong>General</strong> tab.
                </>,
                <>
                  Click <strong>Edit</strong> in{" "}
                  <strong>General Settings</strong>.
                </>,
              ]
            : [
                <>
                  Select <strong>Create App Integration</strong>.
                </>,
                <>
                  Select <strong>OIDC - OpenID Connect</strong>.
                </>,
                <>
                  Choose{" "}
                  <strong>
                    {s.values.oktaAppType === "web"
                      ? "Web Application"
                      : "Native Application"}
                  </strong>
                  .
                </>,
                <>
                  Click <strong>Next</strong>.
                </>,
                <>
                  Enter the name below in <strong>App integration name</strong>.
                </>,
              ]),
          <>
            In <strong>Grant type</strong>, select{" "}
            <strong>Authorization code</strong>.
          </>,
          <>
            Add the URL below to <strong>Sign-in redirect URIs</strong>.
          </>,
          ...(s.values.appChoice === "new"
            ? [
                <>
                  In <strong>Assignments</strong>, select the people or groups
                  who can use this app.
                </>,
              ]
            : []),
          <>Save the app.</>,
        ]}
      />
      {s.values.appChoice === "new" && (
        <CopyValue
          label="App integration name"
          value="Speakeasy AI Control Plane – Okta MCP"
        />
      )}
      {redirect ? (
        <CopyValue label="Sign-in redirect URI" value={redirect} />
      ) : (
        <CallbackUrlStatus
          label="sign-in redirect URI"
          isError={callback.isError}
          retry={() => callback.refetch()}
        />
      )}
      {s.values.appChoice === "existing" && (
        <p className="mt-3 text-sm text-muted-foreground">
          Keep any redirect URIs still needed by existing users.
        </p>
      )}
    </>
  );
}
