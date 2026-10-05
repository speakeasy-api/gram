import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState, type JSX } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  invalidateHooksRollout,
  organizationHooksRolloutQuery,
} from "@/lib/adminQueries";
import {
  clearOrganizationHooksRollout,
  errorMessage,
  setOrganizationHooksRollout,
  type AdminOrganization,
  type AdminOrganizationHooksRollout,
} from "@/lib/gramAdminApi";
import {
  HOOKS_ROLLOUT_PROPAGATION_NOTE,
  validHooksRolloutVersion,
} from "@/lib/hooksRollout";
import { fmtDateShort } from "@/lib/utils";

function sourceSummary(rollout: AdminOrganizationHooksRollout): string {
  switch (rollout.source) {
    case "canary":
      return "Canary organization: always receives the current hooks version, whatever the pins say.";
    case "organization":
      return `Pinned by its own override to hooks version ${rollout.effective_version}.`;
    case "default":
      return `Follows the default pin, hooks version ${rollout.effective_version}.`;
    case "legacy_flag":
      return "No pin applies yet: the legacy hooks-rollout PostHog flag decides. Set the default pin on the Hooks rollout page, or an override here, to take over.";
  }
}

function eligibilityLabel(rollout: AdminOrganizationHooksRollout): string {
  if (rollout.eligible === undefined) return "Unknown";
  return rollout.eligible
    ? `Cleared for version ${rollout.current_version}`
    : `Held below version ${rollout.current_version}`;
}

/**
 * The organization's hooks version rollout state, with its override. The
 * override wins over the default pin in both directions: it can hold an
 * organization back from a release or roll a release out to it early.
 */
export function HooksRolloutOverride({
  org,
}: {
  org: AdminOrganization;
}): JSX.Element {
  const query = organizationHooksRolloutQuery(org.id);
  const { data, isPending, isError } = useQuery({
    ...query,
    enabled: !!org.id,
  });

  if (isPending) {
    return (
      <span className="text-muted-foreground text-sm">
        Loading hooks rollout...
      </span>
    );
  }
  if (!data) {
    return (
      <span className="text-muted-foreground text-sm">
        Unable to load hooks rollout
      </span>
    );
  }
  return (
    <div>
      {isError && (
        <p className="text-muted-foreground mb-3 text-sm">
          Unable to refresh hooks rollout; showing the last loaded state.
        </p>
      )}
      <HooksRolloutOverrideForm
        key={`${data.current_version}-${data.override?.version ?? "none"}`}
        org={org}
        rollout={data}
      />
    </div>
  );
}

function HooksRolloutOverrideForm({
  org,
  rollout,
}: {
  org: AdminOrganization;
  rollout: AdminOrganizationHooksRollout;
}): JSX.Element {
  const queryClient = useQueryClient();
  const queryKey = organizationHooksRolloutQuery(org.id).queryKey;
  const current = rollout.current_version;
  const [value, setValue] = useState(
    String(rollout.override?.version ?? current),
  );
  const version = validHooksRolloutVersion(value, current);

  const onSuccess = (updated: AdminOrganizationHooksRollout) => {
    queryClient.setQueryData(queryKey, updated);
    invalidateHooksRollout(queryClient);
  };
  const setOverride = useMutation({
    mutationFn: (pin: number) =>
      setOrganizationHooksRollout({ organizationID: org.id, version: pin }),
    onSuccess,
  });
  const clearOverride = useMutation({
    mutationFn: () => clearOrganizationHooksRollout(org.id),
    onSuccess,
  });
  const pending = setOverride.isPending || clearOverride.isPending;
  const error = setOverride.error ?? clearOverride.error;

  return (
    <section className="border-border overflow-hidden rounded-md border">
      <div className="border-border bg-muted/20 border-b px-4 py-3">
        <div className="flex items-center gap-2">
          <h5 className="text-sm font-medium">Hooks version rollout</h5>
          <Badge variant="outline">{eligibilityLabel(rollout)}</Badge>
        </div>
        <p className="text-muted-foreground text-sm">
          {sourceSummary(rollout)} {HOOKS_ROLLOUT_PROPAGATION_NOTE}
        </p>
      </div>
      <div className="space-y-3 px-4 py-3">
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 text-sm">
          <dt className="text-muted-foreground">Current hooks version</dt>
          <dd>{current}</dd>
          <dt className="text-muted-foreground">Override</dt>
          <dd>
            {rollout.override
              ? `${rollout.override.version}, set by ${rollout.override.set_by} on ${fmtDateShort(rollout.override.set_at)}`
              : "None"}
          </dd>
          <dt className="text-muted-foreground">Default pin</dt>
          <dd>
            {rollout.default_pin ? rollout.default_pin.version : "Not set"}{" "}
            <Link to="/hooks-rollout" className="text-sm underline">
              Manage
            </Link>
          </dd>
        </dl>
        {rollout.source !== "canary" && (
          <div className="flex items-center gap-2">
            <Input
              className="w-28"
              type="number"
              min={1}
              max={current}
              step={1}
              aria-label="Hooks version override"
              value={value}
              disabled={pending}
              onChange={(event) => setValue(event.target.value)}
            />
            <Button
              size="sm"
              disabled={pending || version === undefined}
              onClick={() => {
                if (version !== undefined) setOverride.mutate(version);
              }}
            >
              {rollout.override ? "Update override" : "Set override"}
            </Button>
            {rollout.override && (
              <Button
                size="sm"
                variant="secondary"
                disabled={pending}
                onClick={() => clearOverride.mutate()}
              >
                Clear override
              </Button>
            )}
          </div>
        )}
        {rollout.source !== "canary" && version === undefined && (
          <p className="text-destructive text-sm">
            The override must be a whole number from 1 to {current}.
          </p>
        )}
        {error && (
          <p className="text-destructive text-sm">{errorMessage(error)}</p>
        )}
      </div>
    </section>
  );
}
