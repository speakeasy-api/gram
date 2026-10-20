import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/Button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { pluralize } from "@/lib/format";
import type { DirectoryReviewItem } from "@gram/client/models/components/directoryreviewitem.js";
import type { Role } from "@gram/client/models/components/role.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useDirectoryReviewItems } from "@gram/client/react-query/directoryReviewItems.js";
import { useResolveDirectoryReviewItemMutation } from "@gram/client/react-query/resolveDirectoryReviewItem.js";

import {
  type ReviewDecision,
  reviewDecisionMessage,
  reviewDecisionRequest,
  reviewItemDetail,
  reviewItemTitle,
} from "./directoryReview";
import {
  choiceFromValue,
  choiceValue,
  proposedChoice,
  type SetupChoice,
} from "./directoryRoleSetup";
import { invalidateDirectoryMappingAccess } from "./invalidateDirectoryMappingAccess";

/**
 * Directory changes waiting for an admin: new groups and attribute values that
 * have no role, and mapped groups or values that have gone. Shows nothing
 * when there is nothing to decide. Each decision saves on its own.
 */
export function DirectoryReviewPanel({
  roles,
  rolesLoaded,
}: {
  /** Roles sorted for display. */
  roles: Role[];
  /**
   * Whether `roles` has loaded. Until it has, a mapped role that isn't in the
   * list is counted rather than called deleted.
   */
  rolesLoaded: boolean;
}): JSX.Element | null {
  const { data, isError, isFetching, refetch } = useDirectoryReviewItems(
    undefined,
    undefined,
    {
      refetchOnMount: "always",
      throwOnError: false,
    },
  );
  const queryClient = useQueryClient();
  const resolve = useResolveDirectoryReviewItemMutation({
    retry: false,
    onError: (error) => {
      toast.error(
        error instanceof GramError && error.statusCode === 409
          ? "This change was already handled or the directory changed. The list is now up to date."
          : error instanceof Error
            ? error.message
            : "Couldn't save the decision. Try again.",
      );
      void invalidateDirectoryMappingAccess(queryClient);
    },
  });
  const roleByUrn = useMemo(
    () => new Map(roles.map((role) => [role.principalUrn, role])),
    [roles],
  );

  if (isError && !data) {
    return (
      <section
        aria-label="Directory changes to review"
        className="border-border flex items-center justify-between gap-3 border px-3 py-2"
      >
        <Text muted small>
          Couldn't load directory changes to review.
        </Text>
        <Button
          variant="secondary"
          size="sm"
          disabled={isFetching}
          onClick={() => void refetch()}
        >
          <Button.Text>Retry</Button.Text>
        </Button>
      </section>
    );
  }

  const items = data?.items ?? [];
  if (items.length === 0) return null;

  // Without the role list, names are unknown, so the detail line counts the
  // roles instead of naming any as deleted.
  const roleNames = (urns: string[]): string[] =>
    rolesLoaded
      ? urns.map((urn) => roleByUrn.get(urn)?.name ?? "a deleted role")
      : [];

  const decide = (
    item: DirectoryReviewItem,
    decision: ReviewDecision,
    choice?: SetupChoice,
  ) =>
    resolve.mutate(
      {
        request: {
          resolveDirectoryReviewItemForm: reviewDecisionRequest(
            item,
            decision,
            choice,
          ),
        },
      },
      {
        onSuccess: () => {
          toast.success(reviewDecisionMessage(item, decision));
          void invalidateDirectoryMappingAccess(queryClient);
        },
      },
    );

  return (
    <section
      aria-label="Directory changes to review"
      className="border-border border"
    >
      <div className="border-border bg-muted/40 border-b px-3 py-2">
        <Text className="font-medium">
          {pluralize(items.length, "directory change")} to review
        </Text>
        <Text muted small>
          Changes in your directory that may need a role. Nothing changes until
          you decide.
        </Text>
      </div>
      {/* Indented under the header with a rule down the side, so the items
          read as entries in its list rather than as more headers. */}
      <ul className="divide-border border-border my-2 mr-3 ml-4 divide-y border-l-2">
        {items.map((item) => (
          <ReviewRow
            key={item.id}
            item={item}
            roles={roles}
            roleNames={roleNames}
            disabled={resolve.isPending}
            onDecide={(decision, choice) => decide(item, decision, choice)}
          />
        ))}
      </ul>
    </section>
  );
}

function ReviewRow({
  item,
  roles,
  roleNames,
  disabled,
  onDecide,
}: {
  item: DirectoryReviewItem;
  roles: Role[];
  roleNames: (urns: string[]) => string[];
  disabled: boolean;
  onDecide: (decision: ReviewDecision, choice?: SetupChoice) => void;
}): JSX.Element {
  const proposed = proposedChoice(item.item);
  // Only an explicit pick is stored. An untouched row follows the proposal
  // as a refetch changes it, and a pick no longer offered (a new role that
  // now exists, or a role since deleted) falls back to the proposal rather
  // than submitting something the server will refuse.
  const [picked, setPicked] = useState<SetupChoice | undefined>(undefined);
  const pickedOffered =
    picked !== undefined &&
    (picked.kind === "create"
      ? proposed !== undefined && choiceValue(proposed) === choiceValue(picked)
      : roles.some((role) => role.principalUrn === picked.roleUrn));
  const choice = pickedOffered ? picked : proposed;
  const missing = item.kind === "source_missing";
  const title = reviewItemTitle(item);

  return (
    <li className="flex flex-wrap items-center gap-3 py-2 pr-1 pl-3">
      {/* The text keeps a readable width; the controls wrap below it. */}
      <div className="min-w-64 flex-1">
        <Text className="truncate font-medium">{title}</Text>
        <Text muted small>
          {reviewItemDetail(item, roleNames)}
        </Text>
        {!missing && choice?.kind === "create" && (
          <Text muted small>
            A new role grants no access until you add permissions to it.
          </Text>
        )}
      </div>
      {/* ml-auto keeps every row's controls on the right, including when
          they wrap below the text. */}
      {missing ? (
        <div className="ml-auto flex items-center gap-2">
          <Button
            variant="secondary"
            size="sm"
            disabled={disabled}
            onClick={() => onDecide("dismiss")}
          >
            <Button.Text>Keep roles</Button.Text>
          </Button>
          <Button
            variant="destructive-secondary"
            size="sm"
            disabled={disabled}
            onClick={() => onDecide("approve")}
          >
            <Button.Text>Remove roles</Button.Text>
          </Button>
        </div>
      ) : (
        <div className="ml-auto flex flex-wrap items-center justify-end gap-2">
          <Select
            value={choice ? choiceValue(choice) : undefined}
            onValueChange={(value) => setPicked(choiceFromValue(value))}
            disabled={disabled}
          >
            <SelectTrigger className="w-56" aria-label={`Role for ${title}`}>
              <SelectValue placeholder="Pick a role" />
            </SelectTrigger>
            <SelectContent>
              {proposed?.kind === "create" && (
                <SelectItem value={choiceValue(proposed)}>
                  New role: {proposed.name}
                </SelectItem>
              )}
              {roles.map((role) => (
                <SelectItem
                  key={role.principalUrn}
                  value={`reuse:${role.principalUrn}`}
                >
                  {role.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            variant="primary"
            size="sm"
            disabled={disabled || !choice}
            onClick={() => onDecide("approve", choice)}
          >
            <Button.Text>Map</Button.Text>
          </Button>
          <Button
            variant="tertiary"
            size="sm"
            disabled={disabled}
            onClick={() => onDecide("dismiss")}
          >
            <Button.Text>Leave unmapped</Button.Text>
          </Button>
          {item.kind === "new_attribute_value" && (
            <Button
              variant="tertiary"
              size="sm"
              disabled={disabled}
              onClick={() => onDecide("opt_out_key")}
            >
              <Button.Text>
                Stop asking about {item.item.source.attributeKey}
              </Button.Text>
            </Button>
          )}
        </div>
      )}
    </li>
  );
}
