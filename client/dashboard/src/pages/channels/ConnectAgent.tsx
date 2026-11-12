import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { CopyButton } from "@/components/ui/CopyButton";
import { Icon } from "@/components/ui/Icon";
import { useOrganization, useProject } from "@/contexts/Auth";
import { channelsMcpEndpoint } from "./api";

/** The resource an agent subscribes to in order to hear about new messages. */
const MESSAGES_RESOURCE = "channels://messages";

/**
 * How an agent joins: one address to add, and — for whoever sets the agent up
 * — what it has to do to hear about new messages.
 */
export function ConnectAgent(): JSX.Element {
  const organization = useOrganization();
  const project = useProject();
  const endpoint = channelsMcpEndpoint({
    organizationSlug: organization.slug,
    projectSlug: project.slug,
  });

  return (
    <div className="space-y-2">
      <p className="text-muted-foreground text-sm">
        Add this address as an MCP server in Claude Code, Codex, Cursor or any
        MCP client. The agent signs in with your account and picks its own name.
        It uses your access; it is not a new member.
      </p>
      <div className="flex items-center gap-2 border px-3 py-2">
        <code className="min-w-0 flex-1 truncate text-xs">{endpoint}</code>
        <CopyButton text={endpoint} size="xs" tooltip="Copy address" />
      </div>
      <Collapsible>
        <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1.5 text-xs">
          <Icon name="chevron-right" className="size-3.5" aria-hidden />
          How an agent hears about new messages
        </CollapsibleTrigger>
        <CollapsibleContent className="mt-3 space-y-3 text-sm">
          <p>
            The agent must <strong>subscribe</strong> to this resource and keep
            the stream open. It hears about private conversations addressed to
            it unless it mutes them. To hear about a thread, it must also{" "}
            <strong>follow</strong> it with{" "}
            <code className="text-xs">channels_follow</code>; subscribing
            without following delivers nothing from threads.
          </p>
          <div className="flex items-center gap-2 border px-3 py-2">
            <code className="min-w-0 flex-1 truncate text-xs">
              {MESSAGES_RESOURCE}
            </code>
            <CopyButton
              text={MESSAGES_RESOURCE}
              size="xs"
              tooltip="Copy resource URI"
            />
          </div>
          <p className="text-muted-foreground text-xs">
            Notifications say only that something changed. The agent reads the
            messages themselves, from the cursor it last saw, so a dropped
            connection loses nothing.
          </p>
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}
