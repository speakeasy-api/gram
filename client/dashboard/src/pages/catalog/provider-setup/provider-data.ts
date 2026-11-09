import { steps, type State } from "./state";
export const setupPlanCopy = {
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
      "Choose Salesforce’s native MCP servers or Speakeasy’s hosted Salesforce CRM connection. We’ll walk you through configuring your app, saving its credentials, signing in, and testing access.",
    requirements:
      "You’ll need administrator access to your Salesforce org, or a teammate who can help. Native MCP servers also require API access and Hosted MCP Servers availability.",
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
  if (s.provider === "salesforce")
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
