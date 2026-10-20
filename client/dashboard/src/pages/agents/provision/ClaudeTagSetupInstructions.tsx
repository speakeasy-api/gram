import { Alert } from "@/components/ui/Alert";
import { Console, Links, VariableField } from "./SetupFields";
import type { Connection, PlatformSetup } from "./setupInstructions";
import { subjectRule, type CatalogEntry } from "./workloadCatalog";

const SETTINGS =
  "https://claude.ai/admin-settings/claude-tag/federated-cloud-access";
const CLAUDE_TAG = "https://claude.ai/admin-settings/claude-tag";
const DOCS =
  "https://claude.com/docs/claude-tag/admins/federated-access/authorization-server";

/**
 * Claude Tag (Claude in Slack).
 *
 * One linear flow that finishes the setup on both sides: Speakeasy learns to
 * trust Anthropic's tokens, then Claude is pointed at the MCP server and told
 * where it may use it. Stopping halfway leaves a setup that looks complete
 * and does nothing, so every step after the rule is saved says what it is for.
 */
export function claudeTagSetupInstructions(entry: CatalogEntry): PlatformSetup {
  const orgId = entry.variables.find((variable) => variable.key === "org_id");

  return {
    connection: (values): Connection | null => {
      const rule = subjectRule(entry, values);
      if (rule === null) return null;
      return {
        name: entry.displayName,
        description: entry.description,
        issuer: entry.issuer.value,
        jwksUri: entry.jwksUri.value,
        subject: rule.subject,
        matchKind: rule.matchKind,
      };
    },
    requirements: (
      <div className="space-y-3 text-sm">
        <p>
          <strong>Claude Tag federated access</strong> in your Anthropic
          organization, and an Owner or an admin with full Claude Tag management
          permission there.
        </p>
        <p>
          The last steps are done in Claude Tag&apos;s own settings. Keep them
          open in another tab.
        </p>
        <Links links={[{ href: DOCS, label: "Claude Tag documentation" }]} />
      </div>
    ),
    steps: [
      {
        id: "organization",
        short: "Organization ID",
        title: "Your Anthropic organization",
        // Collecting and saving in one: the only other thing the save needed
        // was the agent, and this wizard is making it. On its own it was a
        // page with a button and a paragraph saying what the button does.
        phase: "create",
        complete: ({ values }) => (values["org_id"] ?? "").trim().length > 0,
        body: ({ values, setValue, ruleConflict }) => (
          <>
            <Links
              links={[
                {
                  href: "https://claude.ai/admin-settings/organization",
                  label: "Open Claude organization settings",
                },
              ]}
            />
            {orgId !== undefined && (
              <VariableField
                variable={orgId}
                value={values[orgId.key] ?? ""}
                onChange={(next) => setValue(orgId.key, next)}
              />
            )}
            {/* The subject this builds is how Speakeasy recognises Claude's
                tokens. Nobody setting this up has to read it, copy it or keep
                it, so it is not shown — except where it decides something,
                which is when it repeats a rule that already exists. */}
            {ruleConflict !== null && (
              <Alert variant="error" alignTop>
                <div className="text-sm">{ruleConflict}</div>
              </Alert>
            )}
          </>
        ),
      },
      {
        id: "console",
        short: "Connect in Claude",
        title: "Connect Speakeasy in Claude Tag",
        phase: "connect",
        // One sequence, not two steps: it is a single dialog in Claude's own
        // settings, and splitting it across wizard steps asked the reader to
        // carry a half-filled form over a page turn.
        body: ({ computed, issuerPicker }) => (
          <>
            <Console
              rows={[
                {
                  label: "Open Federated agent access in Claude Tag's settings",
                  link: {
                    href: SETTINGS,
                    label: "Federated agent access",
                  },
                },
                {
                  label: (
                    <>
                      Find the <strong>Authorization servers</strong> section
                    </>
                  ),
                },
                {
                  label: (
                    <>
                      Click <strong>Connect an authorization server</strong>
                    </>
                  ),
                },
                {
                  label: "Fill in these values",
                  fields: [
                    {
                      label: "Token endpoint",
                      value: computed.token_endpoint,
                    },
                    {
                      label: "Issuer URL",
                      value: computed.issuer_url,
                      hint: "Claude marks this optional. Fill it in — Claude's token must be issued for this value, or Speakeasy refuses it.",
                    },
                  ],
                },
                {
                  label: (
                    <>
                      Tick{" "}
                      <strong>
                        This authorization server checks that each token&apos;s
                        subject belongs to your organization
                      </strong>
                    </>
                  ),
                  hint: (
                    <>
                      <strong>Register server</strong> stays disabled until you
                      do. Speakeasy makes that check: it is the access rule you
                      just saved, which accepts your organization and refuses
                      every other.
                    </>
                  ),
                },
                {
                  label: (
                    <>
                      Click <strong>Register server</strong>
                    </>
                  ),
                  hint: "The dialog moves to its second half. Closing it here leaves the address registered, counting against Claude's limit of five.",
                },
                {
                  label: "Fill in the second half",
                  fields: [
                    {
                      label: "Resource",
                      hint: "Leave empty. Speakeasy does not scope tokens to one resource.",
                    },
                    { label: "Scope", hint: "Leave empty." },
                    {
                      label: "Allowed API hosts",
                      value: computed.mcp_host,
                      hint: "A leftmost * matches any subdomain.",
                    },
                    {
                      label: "Allowed API paths",
                      hint: "Shown only when the token endpoint is on this same host. List each MCP server's path, such as /mcp/your-server. A listed path covers everything under it.",
                    },
                    {
                      label: "Access bundle",
                      hint: (
                        <>
                          Choose an existing bundle, or click{" "}
                          <strong>New bundle</strong>, name it and click{" "}
                          <strong>Create bundle</strong>. A bundle is Claude's
                          unit of access — a set of connections it may use.
                        </>
                      ),
                    },
                  ],
                },
                {
                  label: (
                    <>
                      Click <strong>Connect server</strong>
                    </>
                  ),
                  hint: "Claude will now attach Speakeasy's token to every request it makes to the hosts above.",
                },
              ]}
            />
            {issuerPicker}
          </>
        ),
      },
      {
        id: "apply",
        short: "Pick where",
        title: "Choose where Claude may use it",
        phase: "connect",
        body: () => (
          <Console
            rows={[
              {
                label: "Open Claude Tag's settings",
                link: { href: CLAUDE_TAG, label: "Claude Tag settings" },
              },
              {
                label: (
                  <>
                    Scroll to <strong>Claude&apos;s access</strong>, open the{" "}
                    <strong>Bundles</strong> tab
                  </>
                ),
              },
              {
                label: (
                  <>
                    Under <strong>Installed</strong>, click your bundle
                  </>
                ),
              },
              {
                label: (
                  <>
                    Under <strong>Where it applies</strong>, turn on your Slack
                    workspace
                  </>
                ),
                hint: "Its channels inherit it. Until you do, the bundle applies nowhere.",
              },
              {
                label: (
                  <>
                    (Optional) Add a system prompt under{" "}
                    <strong>Instructions</strong>
                  </>
                ),
                hint: "How Claude should behave when it uses this bundle.",
              },
            ]}
          />
        ),
      },
      {
        id: "check",
        short: "Test in Slack",
        title: "Check it worked",
        phase: "connect",
        body: () => (
          <div className="space-y-3 text-sm">
            {/* The shape of the thing to type, which is quicker to copy from
                than to describe: the mention, then the question. */}
            <img
              src="/setup/claude-tag-slack-prompt.png"
              alt="A Slack message composer reading: @Claude Tell me about the MCP servers you have access to"
              className="border-border w-full max-w-xl border"
            />
            <p>
              In a Slack <strong>channel</strong> the bundle applies to, start a
              new thread and ask Claude to list the MCP server&apos;s tools. A
              direct message will not work: federated access runs only in
              channel threads.
            </p>
            <p>
              A channel that signs in appears in Speakeasy under the
              agent&apos;s sessions. If nothing arrives, check Claude&apos;s
              error in the thread first — Speakeasy only sees requests that
              reach it.
            </p>
          </div>
        ),
      },
    ],
  };
}
