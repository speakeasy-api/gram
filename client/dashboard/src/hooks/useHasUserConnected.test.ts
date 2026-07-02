import { describe, expect, it } from "vitest";
import { computeHasUserConnected } from "./useHasUserConnected";

function client(id: string) {
  return { id };
}

function session(remoteSessionClientId: string) {
  return { remoteSessionClientId };
}

describe("computeHasUserConnected", () => {
  it("is true when the issuer has no bound clients (nothing to link)", () => {
    expect(computeHasUserConnected([], [])).toBe(true);
    expect(computeHasUserConnected([], [session("c1")])).toBe(true);
  });

  it("is true when every bound client has a session", () => {
    expect(computeHasUserConnected([client("c1")], [session("c1")])).toBe(true);
    expect(
      computeHasUserConnected(
        [client("c1"), client("c2")],
        [session("c2"), session("c1")],
      ),
    ).toBe(true);
  });

  it("is false when any bound client lacks a session", () => {
    expect(computeHasUserConnected([client("c1")], [])).toBe(false);
    expect(
      computeHasUserConnected([client("c1"), client("c2")], [session("c1")]),
    ).toBe(false);
  });

  it("ignores sessions held against unrelated clients", () => {
    expect(computeHasUserConnected([client("c1")], [session("other")])).toBe(
      false,
    );
  });
});
