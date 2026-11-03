import type { Gram as SpeakeasySDK } from "@gram/client";
import { describe, expect, it, vi } from "vitest";
import { keyCredentialChoices } from "./keyCredentialChoices";

const server = { id: "server_one", name: "Example server", kind: "Remote" };
function sdkWith(choices: string[], status?: string) {
  return {
    authClients: {
      listMcpServerChoices: vi.fn().mockResolvedValue({
        choices: choices.map((id) => ({ id })),
        requiresUpstreamAuth: status === "configured",
        status,
      }),
    },
  } as unknown as SpeakeasySDK;
}

describe("keyCredentialChoices", () => {
  it.each([[], ["client_a"], ["client_a", "client_b"]])(
    "records no authentication when the server needs none (%j)",
    async (...ids: string[]) => {
      await expect(
        keyCredentialChoices(sdkWith(ids, "no_auth_required"), [server]),
      ).resolves.toEqual([{ mcpServerId: server.id }]);
    },
  );

  it("keeps an explicitly selected offered credential", async () => {
    await expect(
      keyCredentialChoices(sdkWith(["client_a"], "no_auth_required"), [
        { ...server, authClientId: "client_a" },
      ]),
    ).resolves.toEqual([{ mcpServerId: server.id, authClientId: "client_a" }]);
  });

  it("does not interpret an empty personal-only catalog as no authentication", async () => {
    await expect(
      keyCredentialChoices(sdkWith([], "configured"), [server]),
    ).resolves.toEqual([]);
  });

  it("does not record no authentication while upstream credentials await setup", async () => {
    await expect(
      keyCredentialChoices(sdkWith([], "setup_incomplete"), [server]),
    ).resolves.toEqual([]);
  });

  it("preserves unspecified authentication requirements", async () => {
    await expect(keyCredentialChoices(sdkWith([]), [server])).resolves.toEqual(
      [],
    );
  });

  it.each([undefined, "removed_client"])(
    "requires a current pick when authentication is required (%s)",
    async (authClientId) => {
      await expect(
        keyCredentialChoices(sdkWith(["client_a", "client_b"], "configured"), [
          { ...server, authClientId },
        ]),
      ).rejects.toThrow("Choose which credential");
    },
  );
});
