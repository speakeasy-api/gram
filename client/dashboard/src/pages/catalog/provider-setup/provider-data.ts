import { steps, type State } from "./state";
export const setupPlanCopy = {
  okta: {
    introduction:
      "We’ll walk you through enabling Okta MCP, setting up an app, granting access, and testing the connection.",
    requirements:
      "You’ll need permission to configure an Okta app and grant its API scopes—or a teammate who can help.",
  },
  snowflake: {
    introduction:
      "We’ll walk you through creating or choosing an MCP server, granting access, setting up OAuth, and testing the connection.",
    requirements:
      "Have your existing agent’s database, schema, and name ready, along with a database and schema for the MCP server, a warehouse, and the usernames of the people who will connect. You’ll need permission to configure these—or teammates who can grant access and create the OAuth integration.",
  },
  github: {
    introduction:
      "We’ll walk you through setting up an OAuth app in GitHub, adding its credentials here, and testing the connection.",
    requirements:
      "You’ll need permission to manage OAuth apps in your GitHub organization—or a teammate who can help.",
  },
  slack: {
    introduction:
      "We’ll walk you through creating or configuring a Slack app, choosing its permissions, adding its credentials here, and testing the connection.",
    requirements:
      "You’ll need access to your Slack workspace and permission to manage an app—or a teammate who can help. Your workspace may also require app approval.",
  },
  salesforce: {
    introduction:
      "Choose a Salesforce MCP server and either create your own External Client App or install Speakeasy’s Salesforce app and finish OAuth with support.",
    requirements:
      "You’ll need Salesforce System Administrator access—or a teammate who can help—in an API-enabled production or sandbox org with Hosted MCP Servers available. Salesforce documents availability for Enterprise Edition and above.",
  },
} satisfies Record<
  State["provider"],
  { introduction: string; requirements: string }
>;

const slackScopes = [
  "search:read.public",
  "search:read.private",
  "search:read.mpim",
  "search:read.im",
  "search:read.files",
  "search:read.users",
  "files:read",
  "emoji:read",
  "chat:write",
  "channels:history",
  "groups:history",
  "mpim:history",
  "im:history",
  "channels:write",
  "groups:write",
  "im:write",
  "mpim:write",
  "reactions:write",
  "reactions:read",
  "canvases:read",
  "canvases:write",
  "users:read",
  "users:read.email",
  "channels:read",
  "groups:read",
  "im:read",
  "mpim:read",
  "lists:read",
  "lists:write",
  "files:write",
];
export const scopesFor = (s: { values: { access: string } }): string[] =>
  s.values.access === "read"
    ? slackScopes.filter((x) => !x.includes(":write"))
    : slackScopes;

export function taskTitle(s: State): string {
  if (["salesforce", "snowflake", "okta"].includes(s.provider))
    return (
      steps(s).find((step) => step.id === s.step)?.title ?? "Salesforce setup"
    );
  if (s.step === "begin") return "Setup plan";
  if (s.step === "test") return "Connect and test";
  if (s.step === "credentials") return "Enter app credentials";
  if (s.provider === "github")
    return s.step === "organization"
      ? "Enter your GitHub organization"
      : "Set up OAuth app";
  return s.returnToTest
    ? "Review Slack permissions"
    : s.values.appChoice === "new"
      ? "Create your Slack app"
      : "Configure your Slack app";
}
