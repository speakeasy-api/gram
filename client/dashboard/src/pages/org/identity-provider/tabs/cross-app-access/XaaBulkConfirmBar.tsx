import { Text } from "@/components/ui/Text";
import { pluralize } from "@/lib/format";

import { XaaConfirmFields, type XaaConfirmHandler } from "./XaaConfirmFields";
import type { AppInstanceOption } from "./xaaView";

export function XaaBulkConfirmBar({
  selectedCount,
  applications,
  applicationsUrl,
  pending,
  onConfirm,
}: {
  selectedCount: number;
  applications: AppInstanceOption[];
  applicationsUrl?: string;
  pending: boolean;
  onConfirm: XaaConfirmHandler;
}): JSX.Element {
  return (
    <section className="bg-muted/30 flex flex-col gap-4 border p-4">
      <Text className="font-medium">
        Confirm {pluralize(selectedCount, "selected server")}
      </Text>
      <Text muted small>
        Only confirm servers together if they use the same Okta app and Issuer
        URL. Otherwise, confirm them separately. This saves your confirmation in
        Speakeasy; it does not change or verify Okta.
      </Text>
      <XaaConfirmFields
        applications={applications}
        applicationsUrl={applicationsUrl}
        pending={pending}
        canSubmit={selectedCount > 0}
        submitLabel={`Confirm ${pluralize(selectedCount, "server")}`}
        onConfirm={onConfirm}
      />
    </section>
  );
}
