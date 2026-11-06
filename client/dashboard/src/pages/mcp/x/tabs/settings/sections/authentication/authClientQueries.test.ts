import { QueryClient } from "@tanstack/react-query";
import { expect, it } from "vitest";
import { invalidateAuthClientQueries } from "./authClientQueries";

it("invalidates custom launcher readers alongside generated auth-client queries", async () => {
  const client = new QueryClient();
  const keys = [
    ["launcher-auth-clients", "org-fixture", "project-fixture"],
    ["@gram/client", "authClients", "list"],
  ];
  for (const key of keys) client.setQueryData(key, []);
  await invalidateAuthClientQueries(client);
  for (const key of keys) {
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
  }
  client.clear();
});
