import { createDismissedCtaStore } from "@/hooks/useDismissedCtaStore";
import { useCallback } from "react";

export type OrgHomeAnnouncement = {
  id: string;
  title: string;
  body: string;
  cta: string;
  to: string;
};

/**
 * Flip to true after filling in `ORG_HOME_ANNOUNCEMENT`. Off by default so
 * the bonus card never ships empty or as leftover stub copy.
 */
export const ORG_HOME_ANNOUNCEMENT_ENABLED = false;

/**
 * Copy for the org-home bonus card. Fill every field, then set
 * `ORG_HOME_ANNOUNCEMENT_ENABLED` to true.
 *
 * Example:
 *   id: "explore-demo"
 *   title: "Explore the demo org"
 *   body: "A read-only organization with two weeks of simulated agent traffic."
 *   cta: "Enter demo org"
 *   to: "/explore-demo"
 */
export const ORG_HOME_ANNOUNCEMENT: OrgHomeAnnouncement = {
  id: "",
  title: "",
  body: "",
  cta: "",
  to: "",
};

export function isOrgHomeAnnouncementLive(
  enabled: boolean,
  announcement: OrgHomeAnnouncement,
): boolean {
  return (
    enabled &&
    announcement.id.trim() !== "" &&
    announcement.title.trim() !== "" &&
    announcement.body.trim() !== "" &&
    announcement.cta.trim() !== "" &&
    announcement.to.trim() !== ""
  );
}

export function activeOrgHomeAnnouncement(): OrgHomeAnnouncement | null {
  return isOrgHomeAnnouncementLive(
    ORG_HOME_ANNOUNCEMENT_ENABLED,
    ORG_HOME_ANNOUNCEMENT,
  )
    ? ORG_HOME_ANNOUNCEMENT
    : null;
}

const store = createDismissedCtaStore("gram-org-home-announcement");

function announcementKey(
  orgSlug: string | undefined,
  announcementId: string | undefined,
): string | undefined {
  return orgSlug && announcementId ? `${orgSlug}:${announcementId}` : undefined;
}

export function useOrgHomeAnnouncement(
  orgSlug: string | undefined,
  announcementId: string | undefined,
): {
  dismissed: boolean;
  dismiss: () => void;
} {
  const key = announcementKey(orgSlug, announcementId);
  const dismissed = store.useDismissed(key);
  const dismiss = useCallback(() => {
    if (key) store.write(key, true);
  }, [key]);

  return { dismissed, dismiss };
}
