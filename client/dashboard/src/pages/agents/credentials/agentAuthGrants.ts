import type { Gram } from "@gram/client";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";
import type { KeyCredentialPick } from "./keyCredentialChoices";

export const AUTH_SCOPE = "mcp:auth";

export interface AuthGrant {
  effect: "allow";
  scope: typeof AUTH_SCOPE;
  selector: AgentPolicySelector;
}

/**
 * Sign-in through personal connections, only the represented user's own. It
 * is the default every member holds, so anyone who may create an agent may
 * give it, and the engine never lets it reach another person's connection.
 */
export const PERSONAL_AUTH_GRANT: AuthGrant = {
  effect: "allow",
  scope: AUTH_SCOPE,
  selector: {
    resourceKind: "mcp_auth_client",
    resourceId: "*",
    clientType: "remote_session",
    connectionOwner: "$represented_user",
  },
};

/** Sign-in through one server's shared credential, named for messages. */
export interface SharedAuthGrant {
  grant: AuthGrant;
  serverName: string;
  credentialName: string;
}

/**
 * The shared credential each server signs the agent in with: the one picked
 * for it, or the only one it offers. A server offering several with none
 * picked gets none here; issuing its key asks for the pick. Servers that are
 * neither remote nor tunneled have no upstream to sign in to.
 */
export async function sharedAuthGrants(
  sdk: Gram,
  picks: KeyCredentialPick[],
): Promise<SharedAuthGrant[]> {
  const offered = await Promise.all(
    picks
      .filter((pick) => pick.kind === "Remote" || pick.kind === "Tunneled")
      .map((pick) =>
        sdk.authClients
          .listMcpServerChoices({ mcpServerId: pick.id })
          .then((result) => ({ pick, choices: result.choices }))
          .catch(() => {
            throw new Error(
              `The credentials ${pick.name} offers could not be loaded. Try again.`,
            );
          }),
      ),
  );
  const grants = new Map<string, SharedAuthGrant>();
  for (const { pick, choices } of offered) {
    const choice =
      choices.find((option) => option.id === pick.authClientId) ??
      (choices.length === 1 ? choices[0] : undefined);
    if (!choice || grants.has(choice.id)) continue;
    grants.set(choice.id, {
      grant: {
        effect: "allow",
        scope: AUTH_SCOPE,
        selector: { resourceKind: "mcp_auth_client", resourceId: choice.id },
      },
      serverName: pick.name,
      credentialName: choice.name,
    });
  }
  return [...grants.values()];
}

const SELECTOR_KEYS = [
  "resourceKind",
  "resourceId",
  "clientType",
  "connectionOwner",
  "upstreamClient",
  "projectId",
] as const;

/**
 * Whether the agent already holds this sign-in: the same grant, or the
 * unrestricted one agents set up before these were narrowed were given.
 */
export function holdsAuthGrant(
  held: Iterable<AgentPolicyGrant>,
  wanted: AuthGrant,
): boolean {
  for (const grant of held) {
    if (grant.scope !== AUTH_SCOPE) continue;
    const selector = grant.selector;
    const unrestricted =
      selector.resourceId === "*" &&
      SELECTOR_KEYS.every(
        (key) =>
          key === "resourceKind" || key === "resourceId" || !selector[key],
      );
    if (
      unrestricted ||
      SELECTOR_KEYS.every(
        (key) => (selector[key] ?? "") === (wanted.selector[key] ?? ""),
      )
    ) {
      return true;
    }
  }
  return false;
}
