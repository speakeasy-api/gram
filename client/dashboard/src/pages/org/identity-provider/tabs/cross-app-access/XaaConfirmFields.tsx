import { useState } from "react";

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

import { normalizeAudience, type AppInstanceOption } from "./xaaView";

const NO_APP = "__none__";

export type XaaConfirmValues = {
  audience?: string;
  oktaApplicationId?: string;
};

export type XaaConfirmHandler = (
  audience: string,
  oktaApplicationId: string | undefined,
) => void;

/** The Issuer URL and app inputs shared by the bulk bar and the review panel. */
export function XaaConfirmFields({
  applications,
  applicationsUrl,
  initialValues,
  keepRecordedApp = false,
  pending,
  canSubmit = true,
  submitLabel,
  onConfirm,
  onCancel,
}: {
  /** App instances from the applications snapshot; empty when the snapshot is unavailable. */
  applications: AppInstanceOption[];
  applicationsUrl?: string;
  /** Read once on mount; remount with a key to prefill again. */
  initialValues?: XaaConfirmValues;
  /** Leaving the app unset keeps the one already saved, rather than recording none. */
  keepRecordedApp?: boolean;
  pending: boolean;
  canSubmit?: boolean;
  submitLabel: string;
  onConfirm: XaaConfirmHandler;
  onCancel?: () => void;
}): JSX.Element {
  const isAvailable = (id: string) => applications.some((app) => app.id === id);
  const [audienceInput, setAudienceInput] = useState(
    initialValues?.audience ?? "",
  );
  const [appId, setAppId] = useState(() => {
    const initialAppId = initialValues?.oktaApplicationId;
    if (!initialAppId || (keepRecordedApp && !isAvailable(initialAppId)))
      return NO_APP;
    return initialAppId;
  });
  const recordedAppId = keepRecordedApp
    ? initialValues?.oktaApplicationId
    : undefined;
  const noAppLabel = recordedAppId ? "Keep recorded app" : "Not recorded";
  const audience = normalizeAudience(audienceInput);
  const audienceInvalid = audienceInput.trim() !== "" && audience === undefined;
  const appUnavailable = appId !== NO_APP && !isAvailable(appId);
  const blocked =
    pending || appUnavailable || audience === undefined || !canSubmit;

  const confirm = () => {
    if (blocked || audience === undefined) return;
    onConfirm(audience, appId === NO_APP ? undefined : appId);
  };

  return (
    <>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="xaa-audience">Issuer URL</FieldLabel>
          <Input
            id="xaa-audience"
            disabled={pending}
            onEnter={confirm}
            inputMode="url"
            autoCapitalize="none"
            aria-invalid={audienceInvalid}
            aria-describedby="xaa-audience-help xaa-audience-error"
            value={audienceInput}
            onChange={setAudienceInput}
            placeholder="https://auth.example.com"
            className="font-mono"
            autoComplete="off"
            spellCheck={false}
            error={audienceInvalid}
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
          {audienceInvalid && (
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
            onClick={onCancel}
          >
            Cancel
          </Button>
        )}
        <Button size="sm" disabled={blocked} onClick={confirm}>
          {pending ? "Recording..." : submitLabel}
        </Button>
      </div>
    </>
  );
}
