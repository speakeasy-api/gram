import { describe, expect, it } from "vitest";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import {
  sessionAccountIdentity,
  sessionAccountLabel,
} from "./session-account-identity";

const session = {
  id: "session_example",
  remoteSessionClientId: "client_example",
  userSessionIssuerId: "issuer_example",
  subjectUrn: "user:example",
  subjectDisplayName: "Gram user",
  subjectEmail: "gram@example.test",
  scopes: [],
  hasRefreshToken: false,
  accessExpiresAt: new Date(),
  createdAt: new Date(),
  updatedAt: new Date(),
} satisfies RemoteSession;

describe("stored session account identity", () => {
  it("never substitutes the Gram subject or internal identifiers", () => {
    expect(sessionAccountLabel(session)).toBe("Identity unavailable");
    expect(sessionAccountLabel()).toBe("Identity unavailable");
  });
  it("uses only stored upstream name and email", () => {
    const upstream = {
      ...session,
      upstreamDisplayName: " Example user ",
      upstreamEmail: " upstream@example.test ",
    };
    expect(sessionAccountIdentity(upstream)).toEqual({
      displayName: "Example user",
      email: "upstream@example.test",
    });
    expect(sessionAccountLabel(upstream)).toBe(
      "Example user · upstream@example.test",
    );
  });
  it("handles absent and blank upstream identity without claiming an identity", () => {
    const upstream = {
      ...session,
      upstreamDisplayName: "  ",
      upstreamEmail: "upstream@example.test",
    };
    expect(sessionAccountLabel(upstream)).toBe("upstream@example.test");
  });
});
