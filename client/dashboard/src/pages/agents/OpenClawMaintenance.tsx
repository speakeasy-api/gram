import type { JSX } from "react";
import { CodeBlock } from "./credentials/CodeBlock";

/**
 * Removes what the one-line setup adds to OpenClaw: the hooks plugin, with
 * its settings and files, and the `speakeasy` MCP server entry. They run in a
 * terminal: from a chat, OpenClaw's own `openclaw` tool still asks to confirm
 * the uninstall despite `--force` (2026.10.8), and the call hangs.
 */
const OPENCLAW_UNINSTALL_COMMANDS = [
  "openclaw plugins uninstall speakeasy-observability --force",
  "openclaw config unset mcp.servers.speakeasy",
  "openclaw gateway restart",
].join("\n");

/**
 * How to update, reinstall or remove the OpenClaw setup. OpenClaw does not
 * update the hooks plugin on its own, so a reader who needs a newer one, or
 * needs to stop recording for a while, finds the steps here. It is folded
 * away because most readers are installing for the first time.
 */
export function OpenClawMaintenance(): JSX.Element {
  return (
    <details>
      <summary className="text-muted-foreground hover:text-foreground cursor-pointer text-sm">
        Need to update, reinstall or uninstall on OpenClaw?
      </summary>
      <div className="bg-muted/40 border-muted-foreground/30 mt-2 space-y-3 border p-4 text-sm">
        <p>
          OpenClaw does not update the Speakeasy hooks plugin on its own. To
          update or reinstall it, generate a new setup command here and run it
          again on the same machine. It replaces the plugin and the gateway
          entry, then restart the OpenClaw Gateway. If it says it could not
          configure the gateway, update OpenClaw first: an older release keeps
          the previous entry.
        </p>
        <p>
          To remove Speakeasy from OpenClaw, for example to stop recording its
          chats, run these in a terminal where OpenClaw runs. Do not ask
          OpenClaw to run them from a chat: it waits for a confirmation it
          cannot get.
        </p>
        <CodeBlock
          value={OPENCLAW_UNINSTALL_COMMANDS}
          label="Uninstall from OpenClaw"
          language="shell"
        />
        <p className="text-muted-foreground">
          If OpenClaw runs in a container, run the first two with{" "}
          <code className="text-xs">docker exec</code>, then restart the
          container instead of running{" "}
          <code className="text-xs">openclaw gateway restart</code>. If a
          reinstall fails, uninstall first, then run a new setup command.
        </p>
      </div>
    </details>
  );
}
