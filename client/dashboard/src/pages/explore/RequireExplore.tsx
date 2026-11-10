import { InlineEmptyState } from "@/components/inline-empty-state";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import type { JSX, ReactNode } from "react";

/**
 * Explore and Dashboards are dogfooded before they ship, so the pages are
 * gated as well as their nav entries: hiding the link alone would leave the
 * URL open to anyone who guessed it. The flag is a rollout control, not
 * authorization — the queries behind these pages are scoped by project:read
 * whatever it says.
 */
export function RequireExplore({
  loading,
  children,
}: {
  /** Drawn while PostHog has not answered. */
  loading: ReactNode;
  children: ReactNode;
}): JSX.Element {
  const rollout = useFeatureFlag(FEATURE_FLAGS.explore);

  // PostHog answers after the first paint, so wait rather than telling
  // someone who does have the page that they do not.
  if (rollout.status === "loading") return <>{loading}</>;
  if (rollout.status !== "enabled") {
    return (
      <InlineEmptyState
        icon="telescope"
        heading="This page is not available yet"
        description="It is in preview with a few organizations. Ask your Speakeasy contact to turn it on."
      />
    );
  }
  return <>{children}</>;
}
