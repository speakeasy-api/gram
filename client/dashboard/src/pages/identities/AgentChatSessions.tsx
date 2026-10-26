import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { Link, useLocation } from "react-router";
import { HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import {
  agentSessionHref,
  agentSessionsForAgentHref,
} from "@/pages/chatLogs/agentSessionLink";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
import {
  retryFailed,
  useAgentChats,
  useCanReadAgentChats,
  useIdentityWindow,
} from "./useIdentityQueries";

/** The params the Agent Sessions page reads for its window. */
const WINDOW_PARAMS = ["range", "from", "to"];

/**
 * What the agent did: the sessions its hooks recorded under its key, each a
 * link to its transcript, with the full list a handoff away.
 *
 * These are the agent's work, not its connections — the MCP sessions panel
 * says who is signed in as it, which is a different question.
 */
export function AgentChatSessionsPanel({
  agent,
  limit,
}: {
  agent: ManagedAgent;
  limit: number;
}): JSX.Element {
  const routes = useRoutes();
  const location = useLocation();
  const { from, to } = useIdentityWindow();
  const canRead = useCanReadAgentChats(agent);
  const query = useAgentChats(agent, from, to, limit);
  const chats = query.data?.chats ?? [];
  const total = query.data?.total ?? chats.length;

  const current = new URLSearchParams(location.search);
  const windowParams = new URLSearchParams();
  for (const key of WINDOW_PARAMS) {
    const value = current.get(key);
    if (value) windowParams.set(key, value);
  }
  const agentSessionsHref = routes.agentSessions.href();
  const blocked = query.isError && chats.length === 0;

  return (
    <IdentityPanel
      title="Agent sessions"
      handoffLabel="Agent Sessions"
      handoffHref={agentSessionsForAgentHref(
        agentSessionsHref,
        agent.id,
        windowParams,
      )}
      loading={query.isLoading}
      loadingRows={limit}
      error={blocked}
      refreshFailed={query.isError && !blocked}
      onRetry={retryFailed(query)}
      footer={
        canRead && query.data
          ? `${total} session${total === 1 ? "" : "s"} this period`
          : undefined
      }
    >
      {!canRead ? (
        <IdentityPanelEmpty>
          Listing this agent&rsquo;s sessions needs the chat:read permission or
          ownership of the agent.
        </IdentityPanelEmpty>
      ) : chats.length === 0 ? (
        <IdentityPanelEmpty>
          No sessions from this agent in this window.
        </IdentityPanelEmpty>
      ) : (
        chats.map((chat) => (
          <Link
            key={chat.id}
            to={agentSessionHref(agentSessionsHref, chat.id)}
            className="hover:bg-muted/50 block"
          >
            <IdentityPanelRow
              title={chat.title || "Untitled chat"}
              detail={
                chat.lastMessageTimestamp ? (
                  <HumanizeDateTime date={chat.lastMessageTimestamp} />
                ) : undefined
              }
              trailing={`${chat.numMessages ?? 0} msgs`}
            />
          </Link>
        ))
      )}
    </IdentityPanel>
  );
}
