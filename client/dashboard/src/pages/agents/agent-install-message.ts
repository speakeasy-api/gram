/** A follow-up prompt without the redeemable install code or agent key. */
export function openClawInstallMessage(): string {
  return [
    "I ran the Speakeasy agent install command in a trusted shell and restarted",
    "the OpenClaw Gateway. Check whether the Speakeasy MCP server and",
    "observability plugin are available. Do not read or print credentials.",
  ].join("\n");
}
