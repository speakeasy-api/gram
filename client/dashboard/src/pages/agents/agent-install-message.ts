/**
 * A message to paste into OpenClaw's chat. OpenClaw runs the command in its
 * own shell, so the key never passes through the chat, and the install script
 * ends with a "Speakeasy hooks:" line that says whether the agent's sessions
 * will be recorded.
 */
export function openClawInstallMessage(command: string): string {
  return [
    "Run this command in your shell. Then reply with PASS or FAIL from the line",
    'that starts with "Speakeasy hooks:", and the reason if it failed.',
    "",
    command,
  ].join("\n");
}
