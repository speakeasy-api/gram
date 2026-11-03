import { describe, expect, it } from "vitest";
import {
  upstreamCredentialDetail,
  upstreamCredentialPhrase,
} from "./credential";

describe("upstreamCredentialPhrase", () => {
  it("calls the reader's own connection theirs", () => {
    expect(
      upstreamCredentialPhrase(
        {
          type: "remote_session",
          ownerUserId: "u1",
          ownerName: "Ada Lovelace",
        },
        "u1",
      ),
    ).toBe("your connection (Ada Lovelace)");
  });

  it("names whose connection it was for anyone else", () => {
    expect(
      upstreamCredentialPhrase(
        { type: "remote_session", ownerUserId: "u2", ownerName: "Alex Morgan" },
        "u1",
      ),
    ).toBe("Alex Morgan's connection");
  });

  it("does not show an id when the owner's name did not resolve", () => {
    expect(
      upstreamCredentialPhrase(
        { type: "remote_session", ownerUserId: "u2" },
        "u1",
      ),
    ).toBe("a teammate's connection");
  });

  it("calls a service account or a static credential shared", () => {
    expect(upstreamCredentialPhrase({ type: "service_account" })).toBe(
      "shared credential",
    );
    expect(upstreamCredentialPhrase({ type: "static_credential" })).toBe(
      "shared credential",
    );
  });

  it("leaves a type it does not know to the caller", () => {
    expect(upstreamCredentialPhrase({ type: "something_new" })).toBeUndefined();
  });
});

describe("upstreamCredentialDetail", () => {
  it("keeps the raw auth client reachable", () => {
    expect(
      upstreamCredentialDetail({ type: "service_account", clientId: "c1" }),
    ).toBe("Signed in with: Service account · Auth client c1");
  });
});
