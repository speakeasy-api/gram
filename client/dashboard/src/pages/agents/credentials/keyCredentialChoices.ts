import type { Gram } from "@gram/client";
import type { AgentKeyAuthClientChoice } from "@gram/client/models/components/agentkeyauthclientchoice.js";

/** A server a key will reach, with the credential picked for it, if any. */
export interface KeyCredentialPick {
  id: string;
  name: string;
  kind: string;
  authClientId?: string;
}

/**
 * The credential choices a key is issued with, read against what each remote
 * or tunneled server offers now. A server offering several shared credentials
 * needs the key to name the one it uses; the server would otherwise keep
 * serving it the way it did before credentials could be chosen. Throws an
 * error naming the servers still to choose.
 */
export async function keyCredentialChoices(
  sdk: Gram,
  picks: KeyCredentialPick[],
): Promise<AgentKeyAuthClientChoice[]> {
  const offered = await Promise.all(
    picks
      .filter((pick) => pick.kind === "Remote" || pick.kind === "Tunneled")
      .map((pick) =>
        sdk.authClients
          .listMcpServerChoices({ mcpServerId: pick.id })
          .then((result) => ({
            pick,
            ids: result.choices.map((choice) => choice.id),
          }))
          .catch(() => {
            throw new Error(
              `The credentials ${pick.name} offers could not be loaded. Try again.`,
            );
          }),
      ),
  );
  // A pick the server no longer offers counts as no pick.
  const unchosen = offered.filter(
    ({ pick, ids }) =>
      ids.length > 1 &&
      (!pick.authClientId || !ids.includes(pick.authClientId)),
  );
  if (unchosen.length > 0) {
    throw new Error(
      `Choose which credential the key uses on ${unchosen.map(({ pick }) => pick.name).join(", ")}.`,
    );
  }
  return offered.flatMap(({ pick, ids }) =>
    pick.authClientId && ids.includes(pick.authClientId)
      ? [{ mcpServerId: pick.id, authClientId: pick.authClientId }]
      : [],
  );
}
