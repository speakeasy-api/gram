import type { ServerConnection } from "@/pages/agents/credentials/ownerConnections";

/** The person whose own connection an agent signs in with on a server. */
export type OnBehalfOfPerson = {
  userId: string;
  name: string;
  /** The connection is assigned but no longer works. */
  expired: boolean;
};

const USER_PREFIX = "user:";

/**
 * Whom an agent acts for on a server, from the connection assigned to it
 * there: the owner of a person's own connection. A shared credential, a server
 * that needs no sign-in, and a connection not yet read act for no one.
 *
 * Lives beside `GrantOnBehalfOf` rather than inside it so that file exports
 * only components and keeps fast refresh.
 */
export function onBehalfOfPerson(
  connection: ServerConnection | undefined,
): OnBehalfOfPerson | undefined {
  if (connection?.status !== "ready" && connection?.status !== "ended") {
    return undefined;
  }
  const session = connection.sessions.find((candidate) =>
    candidate.subjectUrn.startsWith(USER_PREFIX),
  );
  if (!session) return undefined;
  const userId = session.subjectUrn.slice(USER_PREFIX.length);
  return {
    userId,
    name: session.subjectDisplayName || session.subjectEmail || userId,
    expired: connection.status === "ended",
  };
}
