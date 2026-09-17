import { describe, expect, it } from "vitest";

import {
  CREATION_SOURCE_UNRECORDED,
  creationSourceLabel,
  isPlatformAdminCreated,
} from "./creationSource";

describe("creationSourceLabel", () => {
  it.each([
    ["platform_admin", "Platform admin"],
    ["signup", "Self-serve signup"],
    ["assistants", "Assistants"],
  ])("names the %s flow", (source, label) => {
    expect(creationSourceLabel(source)).toBe(label);
  });

  // The record has to say something for an organization created before the
  // field existed, and "-" beside "Created via" reads like an answer.
  it.each([undefined, "", "   "])("reports %o as unrecorded", (source) => {
    expect(creationSourceLabel(source)).toBe(CREATION_SOURCE_UNRECORDED);
  });

  // A flow added on the server ships before this map learns about it. Showing
  // the raw value keeps the record truthful in that window; falling back to
  // "Not recorded" would deny a source that exists.
  it("passes an unfamiliar source through", () => {
    expect(creationSourceLabel("partner_referral")).toBe("partner_referral");
  });
});

describe("isPlatformAdminCreated", () => {
  it("recognizes the prospect flow", () => {
    expect(isPlatformAdminCreated("platform_admin")).toBe(true);
  });

  it.each([undefined, "", "signup", "assistants", "platform_admin_invite"])(
    "leaves %o unmarked",
    (source) => {
      expect(isPlatformAdminCreated(source)).toBe(false);
    },
  );
});
