import { Text } from "@/components/ui/Text";

import { OktaLinkButton } from "./OktaLinkButton";
import {
  XaaConfirmFields,
  type XaaConfirmHandler,
  type XaaConfirmValues,
} from "./XaaConfirmFields";
import type { AppInstanceOption } from "./xaaView";

export function XaaReviewPanel({
  serverName,
  confirmed,
  connectionsUrl,
  createUrl,
  applications,
  applicationsUrl,
  initialValues,
  pending,
  onConfirm,
  onCancel,
}: {
  serverName: string;
  /** Editing a saved confirmation rather than recording a new one. */
  confirmed: boolean;
  connectionsUrl?: string;
  createUrl?: string;
  applications: AppInstanceOption[];
  applicationsUrl?: string;
  initialValues?: XaaConfirmValues;
  pending: boolean;
  onConfirm: XaaConfirmHandler;
  onCancel?: () => void;
}): JSX.Element {
  return (
    <section
      aria-label={`Review setup for ${serverName}`}
      className="bg-muted/30 flex flex-col gap-4 border p-4"
    >
      <Text className="font-medium">
        {confirmed ? "Review confirmation" : "Confirm this connection"}
      </Text>
      <Text small className="break-words font-medium">
        {serverName}
      </Text>
      <Text muted small>
        Check for an existing agent connection in Okta first. Reuse it if it
        exists; create a connection only if it is missing. This saves your
        confirmation in Speakeasy; it does not change or verify Okta.
      </Text>
      <div className="flex flex-wrap items-center gap-2">
        {connectionsUrl && (
          <OktaLinkButton href={connectionsUrl} variant="primary">
            Open Okta connections
          </OktaLinkButton>
        )}
        {createUrl && (
          <OktaLinkButton href={createUrl} variant="secondary">
            Create a connection
          </OktaLinkButton>
        )}
      </div>
      <XaaConfirmFields
        applications={applications}
        applicationsUrl={applicationsUrl}
        initialValues={initialValues}
        keepRecordedApp={confirmed}
        pending={pending}
        submitLabel={confirmed ? "Save confirmation" : "Confirm setup"}
        onConfirm={onConfirm}
        onCancel={onCancel}
      />
    </section>
  );
}
