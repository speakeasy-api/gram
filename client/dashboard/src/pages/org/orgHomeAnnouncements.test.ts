import { describe, expect, it } from "vitest";

import {
  ORG_HOME_ANNOUNCEMENT,
  ORG_HOME_ANNOUNCEMENT_ENABLED,
  activeOrgHomeAnnouncement,
  isOrgHomeAnnouncementLive,
  type OrgHomeAnnouncement,
} from "./orgHomeAnnouncements";

const filled: OrgHomeAnnouncement = {
  id: "explore-demo",
  title: "Explore the demo org",
  body: "A read-only organization with simulated traffic.",
  cta: "Enter demo org",
  to: "/explore-demo",
};

describe("isOrgHomeAnnouncementLive", () => {
  it("is off unless enabled and every field is filled", () => {
    expect(isOrgHomeAnnouncementLive(false, filled)).toBe(false);
    expect(isOrgHomeAnnouncementLive(true, filled)).toBe(true);
    expect(isOrgHomeAnnouncementLive(true, { ...filled, title: "  " })).toBe(
      false,
    );
    expect(isOrgHomeAnnouncementLive(true, { ...filled, to: "" })).toBe(false);
  });
});

describe("activeOrgHomeAnnouncement", () => {
  it("is null while the shipped constant is off", () => {
    expect(ORG_HOME_ANNOUNCEMENT_ENABLED).toBe(false);
    expect(ORG_HOME_ANNOUNCEMENT.title).toBe("");
    expect(activeOrgHomeAnnouncement()).toBeNull();
  });
});
