/** The Agent Sessions search param that opens a session's transcript on load. */
export const AGENT_SESSION_CHAT_PARAM = "chatId";

/** The Agent Sessions search param that narrows the list to one agent's sessions. */
export const AGENT_SESSIONS_AGENT_PARAM = "agentId";

/** Link to Agent Sessions with `chatId`'s transcript open. `agentSessionsHref`
 * is the page's own path: `routes.agentSessions.href()`, or one built for
 * another project. */
export function agentSessionHref(
  agentSessionsHref: string,
  chatId: string,
): string {
  return `${agentSessionsHref}?${new URLSearchParams({ [AGENT_SESSION_CHAT_PARAM]: chatId })}`;
}

/** Link to Agent Sessions narrowed to the sessions `agentId` produced.
 * `extra` carries params the destination also reads, such as the date range. */
export function agentSessionsForAgentHref(
  agentSessionsHref: string,
  agentId: string,
  extra: URLSearchParams = new URLSearchParams(),
): string {
  const search = new URLSearchParams(extra);
  search.set(AGENT_SESSIONS_AGENT_PARAM, agentId);
  return `${agentSessionsHref}?${search}`;
}
