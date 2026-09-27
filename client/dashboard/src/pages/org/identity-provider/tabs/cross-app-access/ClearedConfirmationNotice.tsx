import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import type { OktaResourceConnectionServer } from "@gram/client/models/components/oktaresourceconnectionserver.js";

export function ClearedConfirmationNotice({
  snapshot,
  canUndo,
  locked,
  onUndo,
  onReview,
}: {
  snapshot: OktaResourceConnectionServer;
  canUndo: boolean;
  locked: boolean;
  onUndo: () => void;
  onReview: () => void;
}): JSX.Element {
  return (
    <Alert variant="info" alignTop>
      <div className="flex flex-col gap-3">
        <Text small>
          Confirmation cleared for {snapshot.serverName}. The Okta connection
          was not changed.
        </Text>
        <Text muted small>
          Previous settings are retained on this page until you leave or these
          settings are confirmed again. Reuse the existing Okta connection
          rather than creating a duplicate.
        </Text>
        {!canUndo && (
          <Text small>
            Undo is unavailable because the saved Issuer URL or application is
            unavailable. Review setup to choose current settings.
          </Text>
        )}
        <div className="flex flex-wrap gap-2">
          <Button
            variant="secondary"
            size="sm"
            disabled={locked || !canUndo}
            onClick={onUndo}
          >
            Undo
          </Button>
          <Button
            variant="tertiary"
            size="sm"
            disabled={locked}
            onClick={onReview}
          >
            Review setup
          </Button>
        </div>
      </div>
    </Alert>
  );
}
