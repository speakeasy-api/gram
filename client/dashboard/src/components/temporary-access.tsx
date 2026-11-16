import { useUser } from "@/contexts/Auth";
import { useScopeCatalogue } from "@/hooks/useScopeCatalogue";
import { handleError } from "@/lib/errors";
import { cn } from "@/lib/utils";
import { scopeLabel } from "@/pages/access/scopeCatalogue";
import { Button } from "@/components/ui/Button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ResourceKind } from "@gram/client/models/components/selector.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useChallenges } from "@gram/client/react-query/challenges.js";
import { useCreateTemporaryGrantMutation } from "@gram/client/react-query/createTemporaryGrant.js";
import { useRevokeTemporaryGrantMutation } from "@gram/client/react-query/revokeTemporaryGrant.js";
import { useTemporaryGrants } from "@gram/client/react-query/temporaryGrants.js";
import { useQueryClient } from "@tanstack/react-query";
import { Clock } from "lucide-react";
import React from "react";
import {
  challengesAnsweredBy,
  DEFAULT_TEMPORARY_ACCESS_DURATION,
  DURATION_LABELS,
  formatDeadline,
  invalidateTemporaryAccess,
  temporaryAccessDeadline,
  temporaryGrantCovers,
  TEMPORARY_ACCESS_CHALLENGE_LOOKBACK_MS,
  type TemporaryAccessDuration,
} from "./temporary-access-helpers";

type SelfGrantAccessProps = {
  scope: Scope;
  /** The resource the viewer was refused. Omit for every resource. */
  resourceId?: string;
  /**
   * Names the resource for the "covers" choice, e.g. { one: "This session",
   * all: "All sessions" }. Without it the grant covers `resourceId` only.
   */
  coverage?: { one: string; all: string };
  /** Runs after the grant is created, to retry what was refused. */
  onGranted?: () => void;
  className?: string;
};

/**
 * Lets an organization admin give themselves a permission for a few hours,
 * where a page refused it. The grant applies at once and is recorded in the
 * audit log as self-granted; matching denied challenges are resolved with it.
 */
export function SelfGrantAccess(
  props: SelfGrantAccessProps,
): React.JSX.Element {
  // A new resource or scope starts a fresh form.
  return (
    <SelfGrantAccessForm
      key={`${props.scope}|${props.resourceId ?? "*"}`}
      {...props}
    />
  );
}

function SelfGrantAccessForm({
  scope,
  resourceId,
  coverage,
  onGranted,
  className,
}: SelfGrantAccessProps): React.JSX.Element {
  const user = useUser();
  const queryClient = useQueryClient();
  const { catalogue } = useScopeCatalogue();
  const [open, setOpen] = React.useState(false);
  const [covers, setCovers] = React.useState<"one" | "all">("one");
  const [duration, setDuration] = React.useState<TemporaryAccessDuration>(
    DEFAULT_TEMPORARY_ACCESS_DURATION,
  );
  const [reason, setReason] = React.useState("");
  const [conflict, setConflict] = React.useState<string | null>(null);
  // Re-read on every render so the end time shown never goes stale while the
  // form sits open.
  const now = new Date();

  const principalUrn = `user:${user.id}`;
  const resourceKind = catalogue.get(scope)?.resourceType ?? "";
  const grantResourceId =
    resourceId === undefined || covers === "all" ? "*" : resourceId;

  // Only fetched once the form opens: the admin's own recent denials of this
  // scope, to resolve together with the grant. The window starts from when
  // the form opens, not from when the page loaded.
  const [lookbackFrom, setLookbackFrom] = React.useState(
    () => new Date(Date.now() - TEMPORARY_ACCESS_CHALLENGE_LOOKBACK_MS),
  );
  const challengesQuery = useChallenges(
    {
      principalUrn,
      scope,
      outcome: "deny",
      resolved: false,
      from: lookbackFrom,
      limit: 200,
    },
    undefined,
    { enabled: open, throwOnError: false },
  );

  const createGrant = useCreateTemporaryGrantMutation();
  const deadline = temporaryAccessDeadline(duration, now);
  const trimmedReason = reason.trim();
  const canSubmit =
    deadline !== null &&
    trimmedReason.length > 0 &&
    resourceKind !== "" &&
    !createGrant.isPending &&
    !challengesQuery.isLoading;

  const submit = () => {
    if (!canSubmit || deadline === null) return;
    setConflict(null);
    const challengeIds = challengesAnsweredBy(
      challengesQuery.data?.challenges ?? [],
      { principalUrn, scope, resourceKind, resourceId: grantResourceId },
    );
    createGrant.mutate(
      {
        request: {
          createTemporaryGrantForm: {
            principalUrn,
            scope,
            selector: {
              resourceKind: resourceKind as ResourceKind,
              resourceId: grantResourceId,
            },
            expiresAt: deadline,
            reason: trimmedReason,
            challengeIds: challengeIds.length > 0 ? challengeIds : undefined,
          },
        },
      },
      {
        onSuccess: () => {
          invalidateTemporaryAccess(queryClient);
          onGranted?.();
        },
        onError: (error) => {
          if (error instanceof GramError && error.statusCode === 409) {
            // Already held: the page is stale, so refresh what it reads.
            setConflict(
              "You already hold this access. Refreshing the page's permissions.",
            );
            invalidateTemporaryAccess(queryClient);
            return;
          }
          handleError(error, { title: "Could not grant temporary access" });
        },
      },
    );
  };

  if (!open) {
    return (
      <Button
        variant="secondary"
        size="sm"
        className={className}
        onClick={() => {
          setLookbackFrom(
            new Date(Date.now() - TEMPORARY_ACCESS_CHALLENGE_LOOKBACK_MS),
          );
          setOpen(true);
        }}
      >
        <Button.LeftIcon>
          <Clock className="size-3.5" />
        </Button.LeftIcon>
        <Button.Text>Grant myself temporary access</Button.Text>
      </Button>
    );
  }

  const durationOptions = (
    Object.keys(DURATION_LABELS) as TemporaryAccessDuration[]
  ).map((value) => {
    const available = temporaryAccessDeadline(value, now) !== null;
    return {
      value,
      label: DURATION_LABELS[value],
      disabled: !available,
      tooltip: available ? undefined : "The day ends too soon.",
    };
  });

  return (
    <form
      className={cn(
        "bg-card flex w-full flex-col gap-4 border p-4 text-left",
        className,
      )}
      aria-label="Grant myself temporary access"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <div className="flex flex-col gap-1">
        <span className="text-eyebrow">Temporary access</span>
        <p className="text-sm">
          {scopeLabel(catalogue, scope)}{" "}
          <span className="text-muted-foreground font-mono text-xs">
            {scope}
          </span>
        </p>
      </div>

      {coverage && resourceId !== undefined && (
        <Field>
          <FieldLabel>Covers</FieldLabel>
          <SegmentedControl
            ariaLabel="Covers"
            value={covers}
            onChange={setCovers}
            options={[
              { value: "one", label: coverage.one },
              { value: "all", label: coverage.all },
            ]}
          />
        </Field>
      )}

      <Field>
        <FieldLabel>How long</FieldLabel>
        <SegmentedControl
          ariaLabel="How long"
          value={duration}
          onChange={setDuration}
          options={durationOptions}
        />
        <FieldDescription>
          {deadline
            ? `Ends ${formatDeadline(deadline, now)}. You can revoke it earlier.`
            : "Pick another duration."}
        </FieldDescription>
      </Field>

      <Field>
        <FieldLabel htmlFor="self-grant-reason">Reason</FieldLabel>
        <Input
          id="self-grant-reason"
          value={reason}
          onChange={setReason}
          placeholder="What are you investigating?"
          maxLength={1000}
          required
        />
        <FieldDescription>
          Recorded in the audit log with the grant.
        </FieldDescription>
      </Field>

      {conflict && (
        <p className="text-muted-foreground text-sm" role="status">
          {conflict}
        </p>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" variant="primary" size="sm" disabled={!canSubmit}>
          <Button.Text>
            {createGrant.isPending ? "Granting…" : "Grant access"}
          </Button.Text>
        </Button>
        <Button
          type="button"
          variant="tertiary"
          size="sm"
          onClick={() => setOpen(false)}
          disabled={createGrant.isPending}
        >
          <Button.Text>Cancel</Button.Text>
        </Button>
        <span className="text-muted-foreground text-xs">
          Applies to you right away. Logged as self-granted.
        </span>
      </div>
    </form>
  );
}

/**
 * Says the viewer is reading through a temporary grant they hold, when it
 * ends, and lets them give it up. Renders nothing without one.
 */
export function ActiveTemporaryAccess({
  scope,
  resourceId,
  className,
}: {
  scope: Scope;
  resourceId?: string;
  className?: string;
}): React.JSX.Element | null {
  const user = useUser();
  const queryClient = useQueryClient();
  const { data } = useTemporaryGrants(
    { principalUrn: `user:${user.id}` },
    undefined,
    { throwOnError: false, staleTime: 30_000 },
  );
  const revoke = useRevokeTemporaryGrantMutation();
  const grant = data?.grants.find((g) =>
    temporaryGrantCovers(g, scope, resourceId),
  );
  if (!grant) return null;

  const now = new Date();
  return (
    <div
      className={cn(
        "text-muted-foreground flex flex-wrap items-center gap-x-2 gap-y-1 text-xs",
        className,
      )}
    >
      <Clock className="size-3 shrink-0" />
      <span>
        Temporary access · ends {formatDeadline(grant.expiresAt, now)}
      </span>
      <Button
        variant="tertiary"
        size="sm"
        disabled={revoke.isPending}
        onClick={() =>
          revoke.mutate(
            { request: { riskIDRequestBody: { id: grant.id } } },
            {
              onSuccess: () => invalidateTemporaryAccess(queryClient),
              onError: (error) =>
                handleError(error, { title: "Could not revoke access" }),
            },
          )
        }
      >
        <Button.Text>{revoke.isPending ? "Revoking…" : "Revoke"}</Button.Text>
      </Button>
    </div>
  );
}
