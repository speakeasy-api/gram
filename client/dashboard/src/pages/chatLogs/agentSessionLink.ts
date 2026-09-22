/** The Agent Sessions search param that opens a session's transcript on load. */
export const AGENT_SESSION_CHAT_PARAM = "chatId";

/** Link to Agent Sessions with `chatId`'s transcript open. `agentSessionsHref`
 * is the page's own path: `routes.agentSessions.href()`, or one built for
 * another project. */
export function agentSessionHref(
  agentSessionsHref: string,
  chatId: string,
): string {
  return `${agentSessionsHref}?${new URLSearchParams({ [AGENT_SESSION_CHAT_PARAM]: chatId })}`;
}
