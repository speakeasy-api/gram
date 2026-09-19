import { useState } from "react";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { Button } from "@/components/ui/Button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";

import {
  normalizeAudience,
  pluralize,
  type AppInstanceOption,
} from "./connectionView";

export type { AppInstanceOption };

const NO_APP = "__none__";

export function XaaConfirmBar({
  selectedCount,
  applications,
  applicationsUrl,
  onConfirm,
  pending,
  error,
  initialValues,
  review,
  onCancel,
}: {
  selectedCount: number;
  /** App instances from the applications snapshot; empty when the snapshot is unavailable. */
  applications: AppInstanceOption[];
  applicationsUrl?: string;
  onConfirm: (audience: string, oktaApplicationId: string | undefined) => void;
  pending: boolean;
  error: unknown;
  initialValues?: { audience?: string; oktaApplicationId?: string };
  review?: {
    serverName: string;
    confirmed: boolean;
    connectionsUrl?: string;
    createUrl?: string;
  };
  onCancel?: () => void;
}): JSX.Element {
  const [audienceInput, setAudienceInput] = useState(
    initialValues?.audience ?? "",
  );
  const [recordedAppId] = useState(
    review?.confirmed ? initialValues?.oktaApplicationId : undefined,
  );
  const [appId, setAppId] = useState(() => {
    const initialAppId = initialValues?.oktaApplicationId;
    if (
      !initialAppId ||
      (review?.confirmed &&
        !applications.some((app) => app.id === initialAppId))
    )
      return NO_APP;
    return initialAppId;
  });
  const noAppLabel = recordedAppId ? "Keep recorded app" : "Not recorded";
  const audience = normalizeAudience(audienceInput);
  const trimmed = audienceInput.trim();
  const appUnavailable =
    appId !== NO_APP && !applications.some((app) => app.id === appId);

  const confirm = () => {
    if (
      pending ||
      appUnavailable ||
      audience === undefined ||
      selectedCount === 0
    )
      return;
    onConfirm(audience, appId === NO_APP ? undefined : appId);
  };

  return (
    <section
      aria-label={review ? `Review setup for ${review.serverName}` : undefined}
      className="bg-muted/30 flex flex-col gap-4 border p-4"
    >
      <Text className="font-medium">
        {review
          ? review.confirmed
            ? "Review confirmation"
            : "Confirm this connection"
          : `Confirm ${pluralize(selectedCount, "selected server")}`}
      </Text>
      {review ? (
        <>
          <Text small className="break-words font-medium">
            {review.serverName}
          </Text>
          <Text muted small>
            Check for an existing agent connection in Okta first. Reuse it if it
            exists; create a connection only if it is missing. This saves your
            confirmation in Speakeasy; it does not change or verify Okta.
          </Text>
          <div className="flex flex-wrap items-center gap-2">
            {review.connectionsUrl && (
              <Button asChild variant="primary" size="sm">
                <a
                  href={review.connectionsUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Open Okta connections
                </a>
              </Button>
            )}
            {review.createUrl && (
              <Button asChild variant="secondary" size="sm">
                <a
                  href={review.createUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Create a connection
                </a>
              </Button>
            )}
          </div>
        </>
      ) : (
        <Text muted small>
          Only confirm servers together if they use the same Okta app and Issuer
          URL. Otherwise, confirm them separately. This saves your confirmation
          in Speakeasy; it does not change or verify Okta.
        </Text>
      )}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="xaa-audience">Issuer URL</FieldLabel>
          <Input
            id="xaa-audience"
            disabled={pending}
            onEnter={confirm}
            inputMode="url"
            autoCapitalize="none"
            aria-invalid={trimmed !== "" && audience === undefined}
            aria-describedby="xaa-audience-help xaa-audience-error"
            value={audienceInput}
            onChange={setAudienceInput}
            placeholder="https://auth.example.com"
            className="font-mono"
            autoComplete="off"
            spellCheck={false}
            error={trimmed !== "" && audience === undefined}
          />
          <FieldDescription id="xaa-audience-help">
            Open{" "}
            {applicationsUrl ? (
              <a
                href={applicationsUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="underline underline-offset-4"
              >
                Okta Applications
              </a>
            ) : (
              "Okta Applications"
            )}{" "}
            and select the resource app: the app you added from the Okta
            Integration Network for the service this MCP server connects to,
            which Speakeasy is being allowed to reach through Cross App Access.
            It is not one of your Speakeasy apps. Go to Resource Server → Cross
            App Access (XAA); if it is disabled there, enable it first. Copy
            Issuer URL, not the separate Audience/tenant ID. Use the HTTPS
            address from that field, not the MCP server URL.
          </FieldDescription>
          {trimmed !== "" && audience === undefined && (
            <p
              id="xaa-audience-error"
              role="alert"
              className="text-destructive text-sm"
            >
              Enter the HTTPS Issuer URL from Okta, without a ? or # suffix. Do
              not use the MCP server URL.
            </p>
          )}
        </Field>
        <Field>
          <FieldLabel htmlFor="xaa-app-instance">
            Okta application (optional)
          </FieldLabel>
          <Select disabled={pending} value={appId} onValueChange={setAppId}>
            <SelectTrigger
              id="xaa-app-instance"
              aria-describedby="xaa-app-help"
              className="w-full"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={NO_APP}>{noAppLabel}</SelectItem>
              {appUnavailable && (
                <SelectItem value={appId} disabled>
                  Unavailable application
                </SelectItem>
              )}
              {applications.map((app) => (
                <SelectItem key={app.id} value={app.id}>
                  {app.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <FieldDescription id="xaa-app-help">
            {recordedAppId && (
              <>
                Keep recorded app retains the current app; it does not remove
                it.{" "}
              </>
            )}
            {applications.length === 0
              ? recordedAppId
                ? "No applications are available. Keep the recorded app, or sync the Applications tab to pick another."
                : "No applications are available. You can confirm without one, or sync the Applications tab to pick one."
              : "Choose the resource app you copied the Issuer URL from, not a Speakeasy app. This label is saved for reference."}
          </FieldDescription>
          {appUnavailable && (
            <Text small role="alert">
              This application is no longer available. Choose another app or{" "}
              {noAppLabel}.
            </Text>
          )}
        </Field>
      </div>
      <div className="flex items-center justify-end gap-2">
        {onCancel && (
          <Button
            size="sm"
            variant="secondary"
            disabled={pending}
            onClick={() => {
              if (!pending) onCancel();
            }}
          >
            Cancel
          </Button>
        )}
        <Button
          size="sm"
          disabled={
            pending ||
            appUnavailable ||
            audience === undefined ||
            selectedCount === 0
          }
          onClick={confirm}
        >
          {pending
            ? "Recording..."
            : review
              ? review.confirmed
                ? "Save confirmation"
                : "Confirm setup"
              : `Confirm ${pluralize(selectedCount, "server")}`}
        </Button>
      </div>
      <ApiErrorAlert error={error} />
    </section>
  );
}
