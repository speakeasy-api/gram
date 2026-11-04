import type { State } from "./state";
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
  if (s.step === "begin") return "Setup plan";
  if (s.step === "test") return "Connect and test";
  if (s.step === "credentials") return "Enter app credentials";
  return s.returnToTest
    ? "Review Slack permissions"
    : s.values.appChoice === "new"
      ? "Create your Slack app"
      : "Configure your Slack app";
}
