import type { Gram } from "@gram/client";
import type { AgentKeyAuthClientChoice } from "@gram/client/models/components/agentkeyauthclientchoice.js";
import { collectPageItems } from "@/components/sessions/collectPageItems";

/** A server a key will reach, with the credential picked for it, if any. */
export interface KeyCredentialPick {
  id: string;
  name: string;
  kind: string;
  authClientId?: string;
  /** Where the server's sign-in lives, to tell whether it offers accounts. */
  projectSlug?: string;
  issuerId?: string;
}

/**
 * The credential choices a key is issued with, read against what each remote
 * or tunneled server offers now. A server requiring authentication and offering
 * several shared credentials needs the key to name the one it uses. So does a
 * server whose only way in is one shared credential: a key that names none is
 * not served that credential. A server that also offers accounts keeps serving
 * the key through the account connected for the agent unless a shared
 * credential is picked, so a shared credential never replaces it unasked.
 * Throws an error naming the servers still to choose. An explicitly optional
 * upstream records no authentication when no offered credential is selected.
 */
export async function keyCredentialChoices(
  sdk: Gram,
  picks: KeyCredentialPick[],
): Promise<AgentKeyAuthClientChoice[]> {
  const plan = await keyCredentialPlan(sdk, picks, { askWhenAccounts: false });
  if (plan.undecided.length > 0) {
    throw new Error(
      `Choose which credential the key uses on ${plan.undecided.map((pick) => pick.name).join(", ")}.`,
    );
  }
  return plan.choices;
}

/**
 * An explicit pick of the account connected for the agent, on a server that
 * also offers shared credentials. It records no choice, as no pick does, but
 * says the reader made it.
 */
export const ACCOUNT_CHOICE = "account";

/**
 * The credential choices for picks whose credential is decided, and the picks
 * still to decide, by the rules `keyCredentialChoices` describes. With
 * `askWhenAccounts`, a server that offers accounts and shared credentials is
 * also undecided until the account or a shared credential is picked, for a
 * page that asks before the key is extended rather than defaulting to the
 * account.
 */
export async function keyCredentialPlan<P extends KeyCredentialPick>(
  sdk: Gram,
  picks: P[],
  { askWhenAccounts }: { askWhenAccounts: boolean },
): Promise<{
  choices: AgentKeyAuthClientChoice[];
  undecided: P[];
}> {
  const offered = await Promise.all(
    picks
      .filter((pick) => pick.kind === "Remote" || pick.kind === "Tunneled")
      .map((pick) =>
        sdk.authClients
          .listMcpServerChoices({ mcpServerId: pick.id })
          .then((result) => ({
            pick,
            ids: result.choices.map((choice) => choice.id),
            noAuth: result.status === "no_auth_required",
          }))
          .catch(() => {
            throw new Error(
              `The credentials ${pick.name} offers could not be loaded. Try again.`,
            );
          }),
      ),
  );
  // A pick the server no longer offers counts as no pick.
  const picked = ({ pick, ids }: (typeof offered)[number]) =>
    !!pick.authClientId && ids.includes(pick.authClientId);
  const account = ({ pick }: (typeof offered)[number]) =>
    pick.authClientId === ACCOUNT_CHOICE;
  const open = offered.filter(
    (entry) =>
      !entry.noAuth &&
      entry.ids.length > 0 &&
      !picked(entry) &&
      !account(entry),
  );
  const withAccounts = await Promise.all(
    open
      .filter((entry) => entry.ids.length === 1 || askWhenAccounts)
      .map(async (entry) => ({
        entry,
        accounts: await offersAccounts(sdk, entry.pick),
      })),
  );
  const accountsFor = (entry: (typeof offered)[number]) =>
    withAccounts.find((candidate) => candidate.entry === entry)?.accounts ??
    false;
  // Several shared credentials always need a pick. One needs it unless the
  // server also offers accounts, which serve the key when nothing is picked.
  const undecided = open.filter(
    (entry) => entry.ids.length > 1 || askWhenAccounts || !accountsFor(entry),
  );
  return {
    choices: offered.flatMap((entry) =>
      picked(entry)
        ? [
            {
              mcpServerId: entry.pick.id,
              authClientId: entry.pick.authClientId!,
            },
          ]
        : entry.noAuth
          ? [{ mcpServerId: entry.pick.id }]
          : [],
    ),
    undecided: undecided.map((entry) => entry.pick),
  };
}

/**
 * Whether the server's sign-in offers accounts an agent connects. A server
 * without a sign-in offers none.
 */
async function offersAccounts(
  sdk: Gram,
  pick: KeyCredentialPick,
): Promise<boolean> {
  if (!pick.issuerId || !pick.projectSlug) return false;
  try {
    const clients = await collectPageItems(
      sdk.remoteSessionClients.list({
        gramProject: pick.projectSlug,
        userSessionIssuerId: pick.issuerId,
      }),
    );
    // A service account the issuer signs in with itself is a shared
    // credential, not an account anyone connects.
    return clients.some((client) => client.credentialOwner !== "self");
  } catch {
    throw new Error(
      `The accounts ${pick.name} offers could not be loaded. Try again.`,
    );
  }
}
