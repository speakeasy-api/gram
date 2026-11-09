import { useNewRemoteSessionClientCallbackUrl } from "@gram/client/react-query/newRemoteSessionClientCallbackUrl.js";
import { Layers, Server, ShieldCheck, ShieldCog, CopyPlus } from "lucide-react";
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
  Section,
  SetupField,
} from "./shared";
import { salesforceConnection, salesforceServers } from "./salesforce";

/** Provider tasks adapted from the interactive Salesforce demo. */
export function SalesforceTask(): JSX.Element {
  const { s, live } = useSetup();
  const hosted = s.values.integration === "hosted";
  const existing = s.values.appChoice === "existing";
  const connection = salesforceConnection(s.values);
  const callback = useNewRemoteSessionClientCallbackUrl(undefined, undefined, {
    throwOnError: false,
  });
  const redirectUrl = s.saved ? live.callbackUrl : callback.data?.callbackUrl;
  const server = salesforceServers.find(
    (x) => x.id === s.values.salesforceServer,
  );
  switch (s.step) {
    case "begin":
      return (
        <>
          <p>
            Choose Salesforce’s native MCP servers or Speakeasy’s hosted
            Salesforce CRM connection. We’ll walk you through configuring your
            app, saving its credentials, signing in, and testing access.
          </p>
          <p className="mt-5">
            You’ll need administrator access to your Salesforce org, or a
            teammate who can help. Native MCP servers also require API access
            and Hosted MCP Servers availability.
          </p>
        </>
      );
    case "choices":
      return (
        <>
          <Choices
            field="integration"
            label="Salesforce connection"
            options={[
              {
                value: "native",
                title: "Salesforce native MCP",
                description:
                  "Choose a Salesforce-hosted server with the capabilities you need.",
                icon: Server,
              },
              {
                value: "hosted",
                title: "Speakeasy-hosted Salesforce CRM",
                description:
                  "Connect the hosted CRM tools to your Salesforce instance.",
                icon: Layers,
              },
            ]}
          />
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
                  "Requires app installation and setup with support.",
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
          {hosted ? (
            <SetupField
              field="instanceUrl"
              label="Salesforce instance URL"
              placeholder="https://your-org.my.salesforce.com"
            />
          ) : (
            <>
              <label htmlFor="salesforce-server" className="mb-2 block">
                Salesforce server
              </label>
              <select
                id="salesforce-server"
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
            </>
          )}
          <p className="mt-4">
            Use the same org for the app and sign-in. This flow does not cover
            scratch orgs.
          </p>
          {connection && (
            <details className="mt-5">
              <summary>View selected endpoint</summary>
              <code className="mt-3 block break-all">{connection.url}</code>
              {hosted && (
                <p className="mt-3">
                  The instance is saved separately; changing org type does not
                  change this MCP endpoint.
                </p>
              )}
            </details>
          )}
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
            href={
              hosted
                ? (connection?.instanceUrl ?? undefined)
                : s.values.environment === "sandbox"
                  ? "https://test.salesforce.com/"
                  : "https://login.salesforce.com/"
            }
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
            ]}
          />
          {!existing && (
            <>
              <CopyValue label="App Name" value="Speakeasy MCP" />
              <Section title="Contact Email">
                <p>Enter the email of the administrator or app owner.</p>
              </Section>
            </>
          )}
          <Section title="Distribution State">
            <p>
              Use <strong>Local</strong> for the app in this org.
            </p>
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
            <p>Add these to Selected OAuth Scopes:</p>
            <ul className="mt-3 list-disc pl-5">
              <li>
                {hosted ? (
                  <>
                    Manage user data via APIs (<code>api</code>)
                  </>
                ) : (
                  <>
                    Access MCP servers (<code>mcp_api</code>)
                  </>
                )}
              </li>
              <li>
                Perform requests at any time (<code>refresh_token</code>)
              </li>
            </ul>
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
                <strong>Require Secret for Refresh Token Flow</strong> off for a
                Consumer Key-only connection. If your hosted CRM app requires a
                secret, retain that requirement and supply the secret on the
                next credentials step.
              </>,
              <>
                Leave other editable security requirements off, as described in
                the setup guide.
              </>,
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
          <p className="mt-4">
            Salesforce documents up to two minutes for activation. Continue to
            connect and test; only a successful connection test confirms access.
          </p>
        </>
      );
    case "install":
    case "support":
      return (
        <Note>
          The Speakeasy app installation and support completion handoff are not
          configured for this connection yet. This path cannot complete until
          those details are available.{" "}
          <Correction step="choices">Choose another setup method</Correction>.
        </Note>
      );
    default:
      return <></>;
  }
}
