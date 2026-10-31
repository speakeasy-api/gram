import { useSetup } from "./setup-context";
import { names, orgValid } from "./state";
import type { ReactNode } from "react";
import { scopesFor } from "./provider-data";
import { Eye, Link, Settings2 } from "lucide-react";
import {
  AppChoice,
  Choices,
  CopyValue,
  CopyAction,
  ExternalAction,
  InProvider,
  SetupField,
  Note,
  Section,
} from "./shared";
import { useNewRemoteSessionClientCallbackUrl } from "@gram/client/react-query/newRemoteSessionClientCallbackUrl.js";
import { Alert } from "@/components/ui/Alert";
import { slackAppManifest, slackAppCreationUrl } from "./slack";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";

function Plan() {
  const { s } = useSetup();
  const github = s.provider === "github";
  return (
    <>
      <Card className="h-auto bg-muted/10">
        <Card.Content className="grid grid-cols-[20px_minmax(0,1fr)] items-start gap-4">
          <Link className="mt-1 size-5" aria-hidden />
          <div>
            <h3 className="mb-4 text-lg font-normal">
              Let’s connect {names[s.provider]} to Speakeasy.
            </h3>
            <p>
              {github
                ? "We’ll walk you through setting up an OAuth app in GitHub, adding its credentials here, and testing the connection."
                : "We’ll walk you through creating or configuring a Slack app, choosing its permissions, adding its credentials here, and testing the connection."}
            </p>
            <p className="mt-[18px]">
              {github
                ? "You’ll need permission to manage OAuth apps in your GitHub organization—or a teammate who can help."
                : "You’ll need access to your Slack workspace and permission to manage an app—or a teammate who can help. Your workspace may also require app approval."}
            </p>
          </div>
        </Card.Content>
      </Card>
      {github ? (
        <details className="mt-6">
          <summary>Organization approval and SSO</summary>
          <p className="mt-3">
            If your organization restricts OAuth access, an owner will need to
            approve the app. If it enforces SSO, start your SSO session before
            authorizing the app.
          </p>
        </details>
      ) : (
        <details className="mt-6">
          <summary>Using an existing app or an IP allowlist?</summary>
          <p className="mt-3">
            Existing apps must be internal to your workspace or published in the
            Slack Marketplace. If your app uses an IP allowlist, ask your
            network administrator to add Speakeasy’s outbound addresses while
            keeping the restriction in place.
          </p>
        </details>
      )}
    </>
  );
}
function credentialInstructions(
  provider: "slack" | "github",
  existing: boolean,
): ReactNode[] {
  const copyCredentials = (
    <>
      Copy <strong>Client ID</strong> and <strong>Client Secret</strong> into
      the fields below.
    </>
  );
  if (provider === "slack")
    return [
      <>
        Open your app’s <strong>Basic Information → App Credentials</strong>.
      </>,
      copyCredentials,
    ];
  let secretInstruction = (
    <>
      Under <strong>Client secrets</strong>, select{" "}
      <strong>Generate a new client secret</strong>.
    </>
  );
  if (existing)
    secretInstruction = (
      <>
        Use your saved secret, or select{" "}
        <strong>Client secrets → Generate a new client secret</strong>.
      </>
    );
  return [
    <>Open your OAuth app’s settings.</>,
    secretInstruction,
    copyCredentials,
  ];
}
function Credentials() {
  const { s, dispatch, live } = useSetup();
  const saved = !!live && s.saved;
  return (
    <>
      <InProvider
        actions={credentialInstructions(
          s.provider,
          s.values.appChoice === "existing",
        )}
      />
      <SetupField
        field="clientId"
        label="Client ID"
        disabled={saved}
        value={saved ? (live?.savedClientId ?? "") : undefined}
        placeholder="Paste Client ID"
      />
      <SetupField
        field="clientSecret"
        label="Client Secret"
        disabled={saved}
        value={saved ? "••••••••" : undefined}
        placeholder="Paste Client Secret"
      />
      {saved && (
        <div className="mt-5 flex justify-end">
          <Button
            variant="secondary"
            onClick={() => {
              dispatch({
                type: "field",
                field: "clientId",
                value: live?.savedClientId ?? "",
              });
              dispatch({ type: "field", field: "clientSecret", value: "" });
              dispatch({
                type: "connection",
                saved: false,
                authorized: false,
                tested: false,
              });
            }}
          >
            <Button.Text>Update credentials</Button.Text>
          </Button>
        </div>
      )}
    </>
  );
}
function GitHubTask() {
  const { s, live } = useSetup(),
    existing = s.values.appChoice === "existing";
  const callbackQuery = useNewRemoteSessionClientCallbackUrl(
    undefined,
    undefined,
    { throwOnError: false },
  );
  const redirectUrl = s.saved
    ? live?.callbackUrl
    : callbackQuery.data?.callbackUrl;
  if (s.step === "organization")
    return (
      <>
        <p>
          We’ll use this organization to open its OAuth app settings on
          github.com.
        </p>
        <SetupField
          field="org"
          label="GitHub organization"
          placeholder="your-organization"
        />
      </>
    );
  const directory = orgValid(s)
    ? `https://github.com/organizations/${encodeURIComponent(s.values.org.trim())}/settings/applications`
    : "https://github.com/settings/organizations";
  return (
    <>
      <AppChoice />
      <div className="mt-7 min-w-0">
        <ExternalAction
          title={
            existing
              ? "Open an existing GitHub OAuth app"
              : "Create a new GitHub OAuth app"
          }
          label="Open GitHub settings"
          href={directory}
        />
        <InProvider
          actions={[
            existing ? (
              <>Choose the existing OAuth app you want to use.</>
            ) : (
              <>
                Select <strong>New OAuth app</strong>.
              </>
            ),
          ]}
        />
      </div>
      <Section
        title={existing ? "Check the callback URL" : "Configure the app"}
      >
        {!existing && (
          <>
            <CopyValue
              label="Application name"
              value="Speakeasy AI Control Plane – GitHub MCP"
            />
            <CopyValue label="Homepage URL" value="https://www.speakeasy.com" />
          </>
        )}
        {redirectUrl ? (
          <CopyValue label="Authorization callback URL" value={redirectUrl} />
        ) : callbackQuery.isError ? (
          <Alert variant="error">
            Couldn’t load the authorization callback URL.{" "}
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void callbackQuery.refetch()}
            >
              <Button.Text>Retry</Button.Text>
            </Button>
          </Alert>
        ) : (
          <p className="mt-4 text-sm text-muted-foreground">
            Loading authorization callback URL…
          </p>
        )}
        <InProvider
          actions={[
            existing ? (
              <>
                Set <strong>Authorization callback URL</strong> to the value
                above.
              </>
            ) : (
              <>
                Enter the values above in <strong>Application name</strong>,{" "}
                <strong>Homepage URL</strong>, and{" "}
                <strong>Authorization callback URL</strong>.
              </>
            ),
            <>
              Leave <strong>Enable Device Flow</strong> off.
            </>,
          ]}
        />
      </Section>
    </>
  );
}
function SlackTask({ serverName }: { serverName: string }) {
  const { s, live } = useSetup(),
    existing = s.values.appChoice === "existing" || s.returnToTest;
  const callbackQuery = useNewRemoteSessionClientCallbackUrl(
    undefined,
    undefined,
    { throwOnError: false },
  );
  const redirectUrl = s.saved
    ? live?.callbackUrl
    : callbackQuery.data?.callbackUrl;
  const scopes = scopesFor(s);
  const appManifest = redirectUrl
    ? slackAppManifest(scopes, redirectUrl, serverName)
    : undefined;
  const manifest = appManifest
    ? JSON.stringify(appManifest, null, 2)
    : undefined;
  return (
    <>
      {!s.returnToTest && <AppChoice />}
      <Choices
        field="access"
        label="Select Slack permissions"
        className="mb-2"
        options={[
          {
            value: "all",
            title: "Read, search, and write",
            description:
              "Read and search Slack, send messages, and create or update content.",
            icon: Settings2,
          },
          {
            value: "read",
            title: "Read and search only",
            description:
              "Read and search messages, files, channels, and people.",
            icon: Eye,
          },
        ]}
      />
      <p>Access is limited to each person’s existing permissions.</p>
      <details className="mt-3 mb-8" open={existing || undefined}>
        <summary>
          {existing
            ? "User-token scopes to configure in Slack"
            : "Review individual permissions"}
        </summary>
        <div className="mt-4 flex flex-wrap gap-2">
          {scopes.map((scope) => (
            <code className="bg-muted px-2 py-1 text-xs" key={scope}>
              {scope}
            </code>
          ))}
        </div>
      </details>
      {s.configChanged && (
        <Note>
          Permissions changed. Apply the same selection in Slack before
          continuing.
        </Note>
      )}
      <ExternalAction
        title={
          existing
            ? "Open Slack apps"
            : "Create a Slack app with the permissions you selected above."
        }
        label={existing ? "Open Slack apps" : "Create Slack app"}
        href={
          existing
            ? "https://api.slack.com/apps"
            : appManifest
              ? slackAppCreationUrl(appManifest)
              : undefined
        }
        disabled={!existing && !appManifest}
      >
        {existing && <p>Open the app you want to configure.</p>}
      </ExternalAction>
      {!redirectUrl &&
        (callbackQuery.isError ? (
          <Alert variant="error">
            Couldn’t load the redirect URL.{" "}
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void callbackQuery.refetch()}
            >
              <Button.Text>Retry</Button.Text>
            </Button>
          </Alert>
        ) : (
          <p className="mt-4 text-sm text-muted-foreground">
            Loading redirect URL…
          </p>
        ))}
      <InProvider
        actions={
          existing
            ? [
                <>
                  Open your app’s <strong>Agents</strong> page.
                </>,
                <>
                  Turn on{" "}
                  <strong>Slack Model Context Protocol (MCP) Server</strong>.
                </>,
              ]
            : [
                <>Choose your workspace.</>,
                <>Review the prefilled settings.</>,
                <>Create the app.</>,
              ]
        }
      />
      {existing ? (
        <Section title="OAuth permissions and redirect URL">
          <InProvider
            actions={[
              <>
                Open <strong>OAuth &amp; Permissions</strong>.
              </>,
              <>
                Set the user-token scopes to the list above, removing any
                others.
              </>,
              <>
                Under <strong>Redirect URLs</strong>, add the address below.
              </>,
              <>Save the redirect URL.</>,
            ]}
          />
          {redirectUrl && (
            <CopyValue label="Redirect URL" value={redirectUrl} hideLabel />
          )}
          <p className="mt-4">
            Changing permissions may require reinstalling or approving the app
            again.
          </p>
        </Section>
      ) : (
        <details className="mt-6">
          <summary>Can’t use the creation link? Set up manually</summary>
          <InProvider
            actions={[
              <>
                Select <strong>Create New App → From a manifest</strong>.
              </>,
              <>Choose your workspace.</>,
              <>Paste the JSON below and create the app.</>,
            ]}
          />
          {manifest && (
            <>
              <pre className="my-4 border bg-muted p-4 font-mono text-[13px] whitespace-pre-wrap wrap-anywhere">
                {manifest}
              </pre>
              <CopyAction value={manifest} label="Copy app JSON" />
            </>
          )}
        </details>
      )}
    </>
  );
}

export function ProviderTask({
  serverName,
}: {
  serverName: string;
}): JSX.Element {
  const { s } = useSetup();
  if (s.step === "begin") return <Plan />;
  if (s.step === "credentials") return <Credentials />;
  if (s.provider === "github") return <GitHubTask />;
  return <SlackTask serverName={serverName} />;
}
