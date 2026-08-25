import { describe, expect, it } from "vitest";
import { selectExistingClient } from "./selectExistingClient";

describe("selectExistingClient", () => {
  const clients = [{ id: "client-1" }, { id: "client-2" }];

  it("selects the sole client when reusing an issuer", () => {
    expect(selectExistingClient([clients[0]!], "", true)?.id).toBe("client-1");
  });

  it("requires an explicit choice when multiple clients are available", () => {
    expect(selectExistingClient(clients, "", true)).toBeUndefined();
  });

  it("preserves an explicit valid choice", () => {
    expect(selectExistingClient(clients, "client-2", true)?.id).toBe(
      "client-2",
    );
  });

  it("waits for every client page before selecting the sole loaded client", () => {
    expect(selectExistingClient([clients[0]!], "", false)).toBeUndefined();
  });
});
