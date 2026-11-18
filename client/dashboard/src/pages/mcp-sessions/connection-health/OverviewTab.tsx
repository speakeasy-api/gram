import { InlineEmptyState } from "@/components/inline-empty-state";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";

export function OverviewTab({
  provider: _provider,
}: {
  provider: UserSessionIssuer;
}): JSX.Element {
  return <InlineEmptyState icon="layout-dashboard" heading="Overview" />;
}
