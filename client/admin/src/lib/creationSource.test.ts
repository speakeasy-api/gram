import { describe, expect, it } from "vitest";

import {
  CREATION_SOURCE_UNRECORDED,
  creationSourceFact,
} from "./creationSource";

describe("creationSourceFact", () => {
  it.each([
    ["platform_admin", "Platform admin"],
    ["signup", "Self-serve signup"],
    ["assistants", "Assistants"],
  ])("names the %s flow", (source, label) => {
    expect(creationSourceFact(source).label).toBe(label);
  });

  it("marks only the platform-admin flow as the prospect flow", () => {
    expect(creationSourceFact("platform_admin").platformAdmin).toBe(true);
    for (const source of [
      undefined,
      "",
      "signup",
      "assistants",
      "platform_admin_invite",
    ]) {
      expect(creationSourceFact(source).platformAdmin).toBe(false);
    }
  });

  // The record has to say something for an organization created before the
  // field existed, and "-" beside "Created via" reads like an answer.
  it.each([undefined, "", "   "])("reports %o as unrecorded", (source) => {
    const fact = creationSourceFact(source);
    expect(fact.label).toBe(CREATION_SOURCE_UNRECORDED);
    // The record draws an unrecorded source in muted text off this flag. A
    // blank string used to read as recorded while its label said otherwise.
    expect(fact.recorded).toBe(false);
  });

  // A flow added on the server ships before this map learns about it. Showing
  // the raw value keeps the record truthful in that window; falling back to
  // "Not recorded" would deny a source that exists.
  it("passes an unfamiliar source through as recorded", () => {
    const fact = creationSourceFact("partner_referral");
    expect(fact.label).toBe("partner_referral");
    expect(fact.recorded).toBe(true);
    expect(fact.platformAdmin).toBe(false);
  });
});
