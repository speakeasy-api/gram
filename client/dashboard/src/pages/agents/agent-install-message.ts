/** A follow-up prompt without the redeemable install code or agent key. */
export function openClawInstallMessage(): string {
  return [
    "I ran the Speakeasy agent install command in a trusted shell and restarted",
    "the OpenClaw Gateway. Check whether the Speakeasy MCP server is available.",
    'Ask me for only the "Speakeasy hooks:" PASS or FAIL line from the installer.',
    "If it was PASS, also check the observability plugin. If it was FAIL, explain",
    "the reported reason; agents without a project do not get hooks.",
    "Do not read or print credentials.",
  ].join("\n");
}
