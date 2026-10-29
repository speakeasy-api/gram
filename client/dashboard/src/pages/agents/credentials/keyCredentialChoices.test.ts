import type { Gram as SpeakeasySDK } from "@gram/client";
import { describe, expect, it, vi } from "vitest";
import { keyCredentialChoices } from "./keyCredentialChoices";

const server = { id: "server_one", name: "Example server", kind: "Remote" };
function sdkWith(choices: string[], requiresUpstreamAuth?: boolean) {
  return {
    authClients: {
      listMcpServerChoices: vi.fn().mockResolvedValue({
        choices: choices.map((id) => ({ id })),
        requiresUpstreamAuth,
      }),
    },
  } as unknown as SpeakeasySDK;
}

describe("keyCredentialChoices", () => {
  it.each([[], ["client_a"], ["client_a", "client_b"]])(
    "records no authentication when the server explicitly permits it (%j)",
    async (...ids: string[]) => {
      await expect(
        keyCredentialChoices(sdkWith(ids, false), [server]),
      ).resolves.toEqual([{ mcpServerId: server.id }]);
    },
  );

  it("keeps an explicitly selected offered credential", async () => {
    await expect(
      keyCredentialChoices(sdkWith(["client_a"], false), [
        { ...server, authClientId: "client_a" },
      ]),
    ).resolves.toEqual([{ mcpServerId: server.id, authClientId: "client_a" }]);
  });

  it("does not interpret an empty personal-only catalog as no authentication", async () => {
    await expect(
      keyCredentialChoices(sdkWith([], true), [server]),
    ).resolves.toEqual([]);
  });

  it("preserves unspecified authentication requirements", async () => {
    await expect(keyCredentialChoices(sdkWith([]), [server])).resolves.toEqual(
      [],
    );
  });

  it("requires a pick for multiple credentials when authentication is required", async () => {
    await expect(
      keyCredentialChoices(sdkWith(["client_a", "client_b"], true), [server]),
    ).rejects.toThrow("Choose which credential");
  });
});
