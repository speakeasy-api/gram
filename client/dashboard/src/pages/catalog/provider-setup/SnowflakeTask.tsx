import { useNewRemoteSessionClientCallbackUrl } from "@gram/client/react-query/newRemoteSessionClientCallbackUrl.js";
import { CirclePlus, CopyPlus, Globe, GlobeLock } from "lucide-react";
import { useSetup } from "./setup-context";
import {
  Choices,
  Correction,
  CopyAction,
  CopyValue,
  ExternalAction,
  InProvider,
  Section,
  SetupField,
} from "./shared";
import {
  idValid,
  roleAllowed,
  serverValid,
  users,
  snowflakeSql,
} from "./snowflake";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";

function Sql({
  name,
  text,
}: {
  name: string;
  text: string | undefined;
}): JSX.Element | null {
  if (!text) return null;
  return (
    <div className="mt-4" data-sql={name}>
      <CopyAction label="Copy SQL" value={text} />
      <details className="mt-3">
        <summary className="cursor-pointer">View SQL</summary>
        <pre className="mt-4 overflow-x-auto border bg-muted p-4 font-mono text-[13px] leading-relaxed whitespace-pre-wrap wrap-anywhere">
          {text}
        </pre>
      </details>
    </div>
  );
}

export function SnowflakeCredentialInstructions(): JSX.Element {
  const { s } = useSetup();
  const sql = snowflakeSql(s.values);
  if (!sql.credentials)
    return (
      <p>
        Enter the OAuth integration name first.{" "}
        <Correction step="oauth">Review OAuth integration</Correction>.
      </p>
    );
  return (
    <>
      <InProvider
        actions={[
          <>
            Select the integration-owner role in your SQL workspace and run the
            query below.
          </>,
          <>
            Copy <strong>OAUTH_CLIENT_ID</strong> and{" "}
            <strong>OAUTH_CLIENT_SECRET</strong> from their result cells into
            the fields below. Use the primary secret, not OAUTH_CLIENT_SECRET_2.
          </>,
          <>
            Store credentials in your approved password manager, then switch
            away from the integration-owner role. The query result contains
            secrets; keep it out of screenshots and recordings.
          </>,
        ]}
      />
      <Sql name="credentials" text={sql.credentials} />
    </>
  );
}

export function SnowflakeTask(): JSX.Element {
  const { s, live } = useSetup();
  const callbackQuery = useNewRemoteSessionClientCallbackUrl(
    undefined,
    undefined,
    { throwOnError: false },
  );
  const callback = s.saved ? live.callbackUrl : callbackQuery.data?.callbackUrl;
  const v = s.values,
    sql = snowflakeSql(v, callback);
  switch (s.step) {
    case "account":
      return (
        <>
          <ExternalAction
            title="Open your Snowflake account"
            label="Open Snowflake"
            href="https://app.snowflake.com/"
          >
            <p>Sign in to the account that hosts your Cortex Agent.</p>
          </ExternalAction>
          <Section title="Account URL">
            <InProvider
              actions={[
                <>
                  Select your user name, then{" "}
                  <strong>Connect a tool to Snowflake</strong>.
                </>,
                <>
                  Copy <strong>Account/Server URL</strong> into the field below.
                </>,
              ]}
            />
            <SetupField
              field="host"
              label="Public Account/Server URL"
              placeholder="https://my-org-my-account.snowflakecomputing.com"
            />
          </Section>
          <div className="mt-7">
            <Choices
              field="network"
              label="Account connection"
              options={[
                {
                  value: "public",
                  title: "Standard connection",
                  description: "Use the public authorization endpoint.",
                  icon: Globe,
                },
                {
                  value: "private",
                  title: "PrivateLink",
                  description: "Use PrivateLink for browser sign-in.",
                  icon: GlobeLock,
                },
              ]}
            />
          </div>
          <p className="mt-4">
            The MCP server and token exchange must use the public account
            hostname. PrivateLink hostnames are not accepted here.
          </p>
          {v.network === "private" && (
            <p>
              Ask your network owner to confirm the public hostname. Your
              browser must be able to reach the PrivateLink authorization
              endpoint.
            </p>
          )}
          <InProvider
            actions={[
              <>
                Go to <strong>Projects → Workspaces → + → SQL File</strong>.
              </>,
              <>Select an available warehouse.</>,
            ]}
          />
        </>
      );
    case "server":
      return (
        <>
          <Choices
            field="method"
            label="MCP server"
            options={[
              {
                value: "new",
                title: "Create a new server",
                description: "Expose an existing Cortex Agent.",
                icon: CirclePlus,
              },
              {
                value: "existing",
                title: "Use an existing server",
                description:
                  "Connect a server already configured for your agent.",
                icon: CopyPlus,
              },
            ]}
          />
          <Section title="MCP server location">
            <p>
              {v.method === "new"
                ? "Use the database and schema approved by your Snowflake owner. Have the owner create them first if they don’t exist."
                : "Enter the exact database, schema, and name of the existing MCP server."}
            </p>
            <div className="grid gap-x-[18px] sm:grid-cols-2">
              <SetupField
                field="database"
                label="Database"
                placeholder="ANALYTICS_DB"
              />
              <SetupField
                field="schema"
                label="Schema"
                placeholder="MCP_SCHEMA"
              />
            </div>
            <SetupField
              field="serverName"
              label="MCP server name"
              placeholder="SPEAKEASY_MCP"
            />
          </Section>
          <Section title="Cortex Agent">
            <p>
              {v.method === "new"
                ? "Enter the existing agent this server will expose."
                : "Ask the server owner for the Cortex Agent exposed by this server so you can grant access to it."}
            </p>
            <SetupField
              field="agent"
              label="Agent identifier"
              placeholder="DATABASE.SCHEMA.AGENT_NAME"
            />
          </Section>
          {v.method === "new" && (
            <Section title="Tool details">
              <div className="grid gap-x-[18px] sm:grid-cols-2">
                <SetupField field="toolName" label="Tool name" />
                <SetupField field="toolTitle" label="Tool title" />
              </div>
              <SetupField
                field="toolDescription"
                label="Tool description"
                multiline
              />
            </Section>
          )}
        </>
      );
    case "create":
      return (
        <>
          <p>
            Enter the server-creator role to generate the SQL to run in
            Snowflake.
          </p>
          <SetupField
            field="creatorRole"
            label="Server-creator role"
            placeholder="MCP_SERVER_CREATOR"
          />
          {idValid(v.creatorRole) &&
            (!serverValid(v) ? (
              <p className="mt-4">
                <Correction step="server">
                  Enter server details first →
                </Correction>
              </p>
            ) : (
              <>
                <Section title="1. Grant access">
                  <p>Your Snowflake security owner runs this SQL.</p>
                  <Sql name="creator" text={sql.creator} />
                </Section>
                <Section title="2. Create the server">
                  <p>
                    In Snowflake, switch to <strong>{v.creatorRole}</strong> and
                    run this SQL.
                  </p>
                  <Sql name="create" text={sql.create} />
                </Section>
              </>
            ))}
        </>
      );
    case "access":
      return (
        <>
          <p>
            Enter the MCP access role, default warehouse, and connecting
            usernames to generate the SQL to run in Snowflake.
          </p>
          <div className="grid gap-x-[18px] sm:grid-cols-2">
            <SetupField field="accessRole" label="MCP access role" />
            <SetupField
              field="warehouse"
              label="Default warehouse"
              placeholder="COMPUTE_WH"
            />
          </div>
          <SetupField
            field="users"
            label="Connecting Snowflake usernames"
            placeholder="One username per line"
            multiline
          />
          {roleAllowed(v.accessRole) &&
            idValid(v.warehouse) &&
            users(v).length > 0 &&
            users(v).every(idValid) &&
            (!serverValid(v) ? (
              <p className="mt-4">
                <Correction step="server">
                  Enter server details first →
                </Correction>
              </p>
            ) : (
              <>
                <Section title="1. Create the role">
                  <p>
                    With <strong>USERADMIN</strong> or a role with{" "}
                    <strong>CREATE ROLE</strong>, create this role. Skip this
                    statement if the approved role already exists.
                  </p>
                  <Sql name="role" text={sql.role} />
                </Section>
                <Section title="2. Allow Cortex Agent use">
                  <p>
                    With <strong>ACCOUNTADMIN</strong> or a role authorized to
                    grant database roles, grant{" "}
                    <strong>SNOWFLAKE.CORTEX_AGENT_USER</strong>.
                  </p>
                  <Sql name="cortex" text={sql.cortex} />
                </Section>
                <Section title="3. Grant server, warehouse, and agent access">
                  <p>
                    Have the object owners or a role with{" "}
                    <strong>MANAGE GRANTS</strong> run these statements.
                  </p>
                  <Sql name="grants" text={sql.grants} />
                  <p className="mt-3.5">
                    The agent owner must also grant this role access to every
                    resource used by the agent’s tools, such as its search
                    services, semantic views, or tables. Those grants depend on
                    your agent.
                  </p>
                </Section>
                <Section title="4. Assign the role and user defaults">
                  <p>
                    Have the role owner or <strong>MANAGE GRANTS</strong> assign
                    the role. A role authorized to alter each user must set the
                    user defaults.
                  </p>
                  <p className="mt-3">
                    <strong>
                      This changes each listed user’s default role and warehouse
                      for future Snowflake sessions.
                    </strong>{" "}
                    Confirm these changes with your security owner before
                    running the statements.
                  </p>
                  <Sql name="users" text={sql.users} />
                </Section>
              </>
            ))}
        </>
      );
    case "oauth":
      return (
        <>
          <p>
            Enter the integration name to generate the SQL to run in Snowflake.
          </p>
          <SetupField
            field="oauthIntegration"
            label="Integration name"
            placeholder="SPEAKEASY_MCP_OAUTH"
          />
          {idValid(v.oauthIntegration) &&
            (roleAllowed(v.accessRole) ? (
              <>
                <p className="mt-4">
                  Switch the Snowflake workspace role to{" "}
                  <strong>ACCOUNTADMIN</strong> or an approved role with global{" "}
                  <strong>CREATE INTEGRATION</strong>.
                </p>
                <Section title="Required settings">
                  <table className="my-[18px] w-full border-collapse text-left [&_td]:border [&_td]:px-4 [&_td]:py-3">
                    <tbody>
                      <tr>
                        <td>Client type</td>
                        <td>Confidential</td>
                      </tr>
                      <tr>
                        <td>Allowed role</td>
                        <td>{v.accessRole}</td>
                      </tr>
                      <tr>
                        <td>Secondary roles</td>
                        <td>None</td>
                      </tr>
                      {v.network === "private" && (
                        <tr>
                          <td>PrivateLink authorization</td>
                          <td>On</td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                  <p>
                    Each connecting user’s default role must match the allowed
                    role. Privileged administrator roles can’t be used for this
                    connection.
                  </p>
                </Section>
                <Section title="Redirect URL">
                  <p>The integration must use this callback address.</p>
                  {callback ? (
                    <CopyValue
                      label="Redirect URL"
                      value={callback}
                      hideLabel
                    />
                  ) : (
                    <Alert variant={callbackQuery.isError ? "error" : "info"}>
                      {callbackQuery.isError ? (
                        <>
                          Couldn’t load the callback URL.{" "}
                          <Button
                            variant="secondary"
                            onClick={() => void callbackQuery.refetch()}
                          >
                            <Button.Text>Retry</Button.Text>
                          </Button>
                        </>
                      ) : (
                        "Loading callback URL…"
                      )}
                    </Alert>
                  )}
                </Section>
                {sql.oauth && (
                  <Section title="Create the integration in Snowflake">
                    <p>
                      Run the prepared statement. Keep the same
                      integration-owner role selected for the next task.
                    </p>
                    <Sql name="oauth" text={sql.oauth} />
                  </Section>
                )}
              </>
            ) : (
              <p className="mt-4">
                <Correction step="access">
                  Enter the MCP access role first →
                </Correction>
              </p>
            ))}
        </>
      );
    default:
      return <></>;
  }
}
