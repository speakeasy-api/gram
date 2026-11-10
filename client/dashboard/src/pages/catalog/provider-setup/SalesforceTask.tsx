import { useNewRemoteSessionClientCallbackUrl } from "@gram/client/react-query/newRemoteSessionClientCallbackUrl.js";
import { Layers, ShieldCheck, ShieldCog, CopyPlus } from "lucide-react";
import { useSetup } from "./setup-context";
import {
  AppChoice,
  CallbackUrlStatus,
  Choices,
  Correction,
  CopyValue,
  ExternalAction,
  InProvider,
  Note,
  ProviderFieldInstructions,
  Section,
  SetupGroup,
  SetupField,
} from "./shared";
import {
  salesforceConnection,
  salesforceServers,
  salesforceAppInstallationUrl,
  salesforceMyDomain,
} from "./salesforce";

/** Provider instructions follow the current published Salesforce setup guide. */
export function SalesforceTask(): JSX.Element {
  const { s, live } = useSetup();
  const existing = s.values.appChoice === "existing";
  const connection = salesforceConnection(s.values);
  const callback = useNewRemoteSessionClientCallbackUrl(undefined, undefined, {
    throwOnError: false,
  });
  const redirectUrl = s.saved ? live.callbackUrl : callback.data?.callbackUrl;
  const server = salesforceServers.find(
    (x) => x.id === s.values.salesforceServer,
  );
  const basicFields: Parameters<typeof ProviderFieldInstructions>[0]["items"] =
    [
      {
        label: "Distribution State",
        description: (
          <>
            Select <strong>Local</strong> for the app in this org.
          </>
        ),
      },
    ];
  if (!existing)
    basicFields.unshift(
      {
        label: "App Name",
        description:
          "Enter a descriptive name for the app in Salesforce. You can use the suggested name below.",
        copyValue: "Speakeasy MCP",
      },
      {
        label: "Contact Email",
        description: "Enter the email of the administrator or app owner.",
      },
    );
  switch (s.step) {
    case "choices":
      return (
        <>
          <Choices
            field="path"
            label="Setup method"
            options={[
              {
                value: "own",
                title: "Use your own app",
                description:
                  "Create or configure an app in your Salesforce org.",
                icon: Layers,
              },
              {
                value: "support",
                title: "Use Speakeasy’s app",
                description:
                  "Install Speakeasy’s Salesforce app, then finish OAuth with support.",
                icon: CopyPlus,
              },
            ]}
          />
          <Choices
            field="environment"
            label="Salesforce org type"
            options={[
              {
                value: "production",
                title: "Production",
                description: "Connect your live Salesforce org.",
                icon: ShieldCheck,
              },
              {
                value: "sandbox",
                title: "Sandbox",
                description: "Connect a Salesforce testing org.",
                icon: ShieldCog,
              },
            ]}
          />
          <SetupGroup
            label="Salesforce server"
            labelId="salesforce-server-label"
          >
            <select
              id="salesforce-server"
              aria-labelledby="salesforce-server-label"
              className="h-10 w-full border bg-background px-3"
              value={s.values.salesforceServer}
              onChange={(event) =>
                live.changeField({
                  field: "salesforceServer",
                  value: event.target.value,
                })
              }
            >
              {salesforceServers.map((x) => (
                <option key={x.id} value={x.id}>
                  {x.title}
                </option>
              ))}
            </select>
            <p className="mt-3">{server?.description}</p>
            {!!server?.prerequisites.length && (
              <Section title="Before you connect">
                <ul className="list-disc pl-5">
                  {server.prerequisites.map((requirement) => (
                    <li key={requirement}>{requirement}</li>
                  ))}
                </ul>
              </Section>
            )}
          </SetupGroup>
          <p className="mt-4">
            Use the same org for the app and sign-in. This flow does not cover
            scratch orgs.
          </p>
          {connection && (
            <details className="mt-5">
              <summary>View selected endpoint</summary>
              <code className="mt-3 block break-all">{connection.url}</code>
            </details>
          )}
        </>
      );
    case "salesforce-org":
      return (
        <>
          <SetupField
            field="salesforceDomain"
            label="Salesforce Domain (optional)"
            onBlur={() => {
              const domain = salesforceMyDomain(
                s.values.salesforceDomain,
                s.values.environment,
              );
              if (domain && domain.origin !== s.values.salesforceDomain)
                live.changeField({
                  field: "salesforceDomain",
                  value: domain.origin,
                });
            }}
            placeholder={
              s.values.environment === "sandbox"
                ? "example--uat.sandbox.my.salesforce.com"
                : "example.my.salesforce.com"
            }
          />
          <p className="mt-4">
            If your org requires signing in through its own domain. Leave this
            blank to use standard login.
          </p>
        </>
      );
    case "app":
      return (
        <>
          <AppChoice
            descriptions={[
              "Register a new External Client App in your Salesforce org.",
              "Configure an External Client App you already manage in that org.",
            ]}
          />
          <ExternalAction
            title="Open Salesforce"
            label="Open Salesforce"
            href={connection?.issuer}
          />
          <InProvider
            actions={[
              <>
                Open <strong>Setup → External Client App Manager</strong>.
              </>,
              existing ? (
                <>Open the app you want to use.</>
              ) : (
                <>
                  Select <strong>New External Client App</strong>.
                </>
              ),
              <>
                In Salesforce’s <strong>Basic Information</strong> section,
                enter the app details shown below.
              </>,
            ]}
          />
          <Section title="Basic information">
            <ProviderFieldInstructions items={basicFields} />
          </Section>
        </>
      );
    case "oauth":
      return (
        <>
          <InProvider
            actions={[
              <>
                Expand <strong>API (Enable OAuth Settings)</strong> and enable
                OAuth.
              </>,
              <>
                Set <strong>Callback URL</strong> to the address below.
              </>,
              <>
                Select each scope shown below in{" "}
                <strong>Available OAuth Scopes</strong>, then use the right
                arrow to move it to <strong>Selected OAuth Scopes</strong>.
              </>,
            ]}
          />
          {redirectUrl ? (
            <CopyValue label="Callback URL" value={redirectUrl} />
          ) : (
            <CallbackUrlStatus
              label="callback URL"
              isError={callback.isError}
              retry={() => callback.refetch()}
            />
          )}
          <Section title="OAuth scopes">
            <ProviderFieldInstructions
              items={[
                {
                  label: "Access MCP servers (mcp_api)",
                  description:
                    "Allows the app to access Salesforce MCP servers on your behalf.",
                },
                {
                  label: "Perform requests at any time (refresh_token)",
                  description:
                    "Allows the connection to refresh access without asking you to sign in again each time an access token expires.",
                },
              ]}
            />
          </Section>
        </>
      );
    case "security":
      return (
        <>
          <InProvider
            actions={[
              <>
                Open the app’s <strong>Security</strong> settings.
              </>,
              <>
                Enable{" "}
                <strong>
                  Issue JSON Web Token (JWT)-based access tokens for named users
                </strong>
                .
              </>,
              <>
                Leave <strong>Require Secret for Web Server Flow</strong> and{" "}
                <strong>Require Secret for Refresh Token Flow</strong> off
              </>,
              <>Leave other editable security requirements off</>,
              existing ? (
                <>Save the changes.</>
              ) : (
                <>
                  Select <strong>Create</strong>.
                </>
              ),
            ]}
          />
          {!existing && (
            <Note>
              The app can take up to 30 minutes to become operational. If
              sign-in fails immediately, allow that time before changing the
              configuration.
            </Note>
          )}
        </>
      );
    case "activation":
      return (
        <>
          {!!server?.prerequisites.length && (
            <Note>{server.prerequisites.join(" ")}</Note>
          )}
          <InProvider
            actions={[
              <>
                Open <strong>Setup → API Catalog → MCP Servers</strong>.
              </>,
              <>
                Find <strong>{server?.title}</strong> (
                <code>{s.values.salesforceServer}</code>).
              </>,
              <>
                Enable that server in the same org as your External Client App.
              </>,
            ]}
          />
          <p className="mt-4">Activation can take up to two minutes.</p>
        </>
      );
    case "install":
      return (
        <>
          <ExternalAction
            title="Install Speakeasy’s Salesforce app"
            label="Open Salesforce installation"
            href={salesforceAppInstallationUrl(
              s.values.environment,
              s.values.salesforceDomain,
            )}
          />
          <InProvider
            actions={[
              <>
                Install Speakeasy’s Salesforce application into the intended
                org.
              </>,
              <>
                After installation, contact Speakeasy support to finish OAuth
                and coordinate the selected endpoint and server activation.
              </>,
            ]}
          />
          <Note>
            Installation alone does not complete OAuth. Continue for the support
            handoff instructions; this page does not verify installation.
          </Note>
        </>
      );
    case "support":
      return (
        <>
          <InProvider
            actions={[
              <>Contact Speakeasy support to finish OAuth setup.</>,
              <>
                Share the selected endpoint below and coordinate server
                activation in the same Salesforce org.
              </>,
            ]}
          />
          {connection && (
            <CopyValue label="Selected MCP endpoint" value={connection.url} />
          )}
          <ExternalAction
            title="Support handoff"
            label="Read support instructions"
            href="https://www.speakeasy.com/docs/ai-control-plane/guides/salesforce#contact-speakeasy-support"
          />
          <Note>
            Your next step is with Speakeasy support. This page has no verified
            completion signal for that handoff and will not mark the connection
            complete.{" "}
            <Correction step="choices">Choose another setup method</Correction>.
          </Note>
        </>
      );
    default:
      return <></>;
  }
}
