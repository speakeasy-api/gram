import { useState } from "react";
import { ArrowDown, ArrowUp, X } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { SettingsSection } from "@/components/detail/settings-section";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import type { EMAProviderInput } from "@gram/client/models/components/emaproviderinput.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { invalidateAllOrganizationRemoteSessionIssuerEMAServices } from "@gram/client/react-query/organizationRemoteSessionIssuerEMAServices.js";
import { useSetOrganizationRemoteSessionIssuerEMAProvidersMutation } from "@gram/client/react-query/setOrganizationRemoteSessionIssuerEMAProviders.js";

import {
  DEFAULT_ORDER_RULE,
  PLACEHOLDER_SCOPE,
  providersBeforeSpeakeasy,
  SPEAKEASY_DEFAULT_SCOPE,
  speakeasyFallbackNote,
  type ProviderView,
} from "./emaServiceView";

/** The API's limit on an order's length. */
const MAX_ORDER_LENGTH = 10;

/** One identity provider an admin can put in the order. */
export type OrderCandidate = {
  key: string;
  kind: "external" | "speakeasy";
  name: string;
  /** The external provider's issuer record. */
  remoteSessionIssuerId?: string;
};

type Entry = OrderCandidate & {
  /**
   * False when a saved entry can no longer be referenced: deleted, or no
   * organization sign-in trusts it. One that is only paused or disabled
   * stays valid and gets a readiness note instead.
   */
  available: boolean;
};

/**
 * Which identity providers are tried for a service, in what order. By
 * default each server is vouched for by whatever signs its people in. A
 * saved order applies to every server that reaches the service. Key it by
 * service so a draft never carries over to another one.
 */
export function EmaProviderOrderEditor({
  serviceName,
  remoteSessionIssuerId,
  current,
  isDefault,
  candidates,
  candidatesReady,
  canEdit,
  canRestore,
  readOnlyReason,
}: {
  serviceName: string;
  /** Any issuer record naming the service; the API keys the order by it. */
  remoteSessionIssuerId: string | undefined;
  current: ProviderView[];
  isDefault: boolean;
  candidates: OrderCandidate[];
  /** Whether every candidate has loaded, so a saved entry missing from them is really gone. */
  candidatesReady: boolean;
  /** Build and save a custom order. */
  canEdit: boolean;
  /** Go back to the default. */
  canRestore: boolean;
  /** Why the person can't edit, when they can't. */
  readOnlyReason?: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<Entry[] | null>(null);
  // The saved order the draft started from, so a change underneath it stops
  // Save instead of silently restoring what another admin removed.
  const [base, setBase] = useState<Entry[] | null>(null);
  const [conflict, setConflict] = useState(false);
  const [adding, setAdding] = useState("");
  const saved: Entry[] = isDefault
    ? []
    : current.map((provider) => {
        const candidate = candidates.find((c) => c.key === provider.key);
        return {
          key: provider.key,
          kind: provider.kind,
          name: candidate?.name ?? provider.name,
          remoteSessionIssuerId: provider.remoteSessionIssuerId,
          available:
            !candidatesReady ||
            (!!candidate && provider.unavailableReason !== "deleted"),
        };
      });
  const readiness = (key: string): string | undefined => {
    const provider = current.find((p) => p.key === key);
    return provider && !provider.usable && !provider.placeholder
      ? provider.unavailable
      : undefined;
  };
  const startEditing = (entries: Entry[]) => {
    setBase(saved);
    setConflict(false);
    setDraft(entries);
  };
  const stopEditing = () => {
    setDraft(null);
    setBase(null);
    setConflict(false);
  };
  const editing = draft !== null;
  const order = draft ?? saved;
  const changedUnderneath =
    editing && (conflict || (base !== null && !sameOrder(base, saved)));
  const custom = editing || !isDefault;
  const dirty = editing && (isDefault || !sameOrder(draft, saved));
  const full = order.length >= MAX_ORDER_LENGTH;
  const addable = candidates.filter(
    (candidate) => !order.some((entry) => entry.key === candidate.key),
  );
  const unavailable = order.filter((entry) => !entry.available);
  const fallbackFrom = providersBeforeSpeakeasy(order);

  const save = useSetOrganizationRemoteSessionIssuerEMAProvidersMutation({
    onSuccess: async (result) => {
      // Refresh before dropping the draft, so the saved order never flickers.
      await Promise.all([
        invalidateAllOrganizationRemoteSessionIssuerEMAServices(queryClient),
        queryClient.invalidateQueries({
          queryKey: ["identityChaining", "readEMA"],
        }),
      ]);
      stopEditing();
      toast.success(
        result.isDefault
          ? `${serviceName} uses the default order`
          : `Saved the order for ${serviceName}`,
      );
    },
    onError: (error) => {
      if (error instanceof GramError && error.statusCode === 409) {
        setConflict(true);
        void invalidateAllOrganizationRemoteSessionIssuerEMAServices(
          queryClient,
        );
      }
    },
  });
  const submit = (entries: Entry[]) => {
    if (!remoteSessionIssuerId) return;
    save.mutate({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId,
          providers: entries.map(providerInput),
          expectedProviders: (base ?? saved).map(providerInput),
        },
      },
    });
  };
  const conflictError =
    save.error instanceof GramError && save.error.statusCode === 409;
  const move = (index: number, by: -1 | 1) => {
    const next = [...order];
    const [entry] = next.splice(index, 1);
    if (!entry) return;
    next.splice(index + by, 0, entry);
    setDraft(next);
  };
  // Start from what runs today: each identity provider a sign-in trusts,
  // then Speakeasy when it exists.
  const customize = () =>
    startEditing(
      [
        ...candidates.filter((c) => c.kind === "external"),
        ...candidates.filter((c) => c.kind === "speakeasy"),
      ]
        .slice(0, MAX_ORDER_LENGTH)
        .map((c) => ({ ...c, available: true })),
    );
  const showEditControls = canEdit && editing;

  return (
    <SettingsSection id="order">
      <SettingsSection.Header>
        <SettingsSection.Title>Identity provider order</SettingsSection.Title>
        <SettingsSection.Description>
          Which identity provider vouches for people when they connect to{" "}
          {serviceName}, and which is tried next if one refuses or fails.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {!custom ? (
            <div className="space-y-3">
              <Text small>{DEFAULT_ORDER_RULE}</Text>
              <ul aria-label="Default order" className="space-y-1">
                {current.map((provider) => (
                  <li
                    key={provider.key}
                    className="flex flex-wrap items-baseline gap-x-2"
                  >
                    <Text as="span" small className="font-medium">
                      {provider.name}
                    </Text>
                    <Text as="span" muted small>
                      {provider.kind === "speakeasy"
                        ? SPEAKEASY_DEFAULT_SCOPE
                        : PLACEHOLDER_SCOPE}
                    </Text>
                  </li>
                ))}
              </ul>
            </div>
          ) : (
            <>
              <Text muted small>
                A saved order applies to every server that reaches {serviceName}
                , in place of whatever signs each server’s people in.
              </Text>
              {changedUnderneath && (
                <Alert variant="warning" alignTop>
                  <div className="flex flex-col items-start gap-2">
                    <Text small className="block">
                      This order changed since you started editing.{" "}
                      {saved.length > 0
                        ? `It’s now: ${saved.map((entry) => entry.name).join(", ")}.`
                        : `${serviceName} uses the default order now.`}
                    </Text>
                    <div className="flex flex-wrap gap-2">
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={stopEditing}
                      >
                        Discard my changes
                      </Button>
                      <Button
                        variant="tertiary"
                        size="sm"
                        onClick={() => {
                          setBase(saved);
                          setConflict(false);
                          save.reset();
                        }}
                      >
                        Review and keep editing
                      </Button>
                    </div>
                  </div>
                </Alert>
              )}
              {order.length === 0 ? (
                <Text small>Add at least one identity provider.</Text>
              ) : (
                <ol aria-label="Provider order" className="space-y-2">
                  {order.map((entry, index) => (
                    <li key={entry.key} className="flex items-center gap-3">
                      <Text
                        as="span"
                        muted
                        small
                        className="w-5 text-right tabular-nums"
                      >
                        {index + 1}
                      </Text>
                      <span className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2">
                        <Text as="span" small className="truncate font-medium">
                          {entry.name}
                        </Text>
                        {!entry.available ? (
                          <Text
                            as="span"
                            small
                            className="text-default-warning"
                          >
                            No longer available. Remove it to save.
                          </Text>
                        ) : (
                          readiness(entry.key) && (
                            <Text as="span" muted small>
                              {readiness(entry.key)}. It’s skipped until it can
                              vouch again.
                            </Text>
                          )
                        )}
                      </span>
                      {showEditControls && (
                        <div className="flex items-center gap-1">
                          <Button
                            variant="tertiary"
                            size="sm"
                            aria-label={`Move ${entry.name} up`}
                            disabled={index === 0 || save.isPending}
                            onClick={() => move(index, -1)}
                          >
                            <Button.Icon>
                              <ArrowUp className="size-4" />
                            </Button.Icon>
                          </Button>
                          <Button
                            variant="tertiary"
                            size="sm"
                            aria-label={`Move ${entry.name} down`}
                            disabled={
                              index === order.length - 1 || save.isPending
                            }
                            onClick={() => move(index, 1)}
                          >
                            <Button.Icon>
                              <ArrowDown className="size-4" />
                            </Button.Icon>
                          </Button>
                          <Button
                            variant="tertiary"
                            size="sm"
                            aria-label={`Remove ${entry.name}`}
                            disabled={save.isPending}
                            onClick={() =>
                              setDraft(order.filter((_, i) => i !== index))
                            }
                          >
                            <Button.Icon>
                              <X className="size-4" />
                            </Button.Icon>
                          </Button>
                        </div>
                      )}
                    </li>
                  ))}
                </ol>
              )}
              {fallbackFrom.length > 0 && (
                <Text muted small>
                  {speakeasyFallbackNote(fallbackFrom)}
                </Text>
              )}
              {showEditControls &&
                (full ? (
                  <Text muted small>
                    An order holds at most {MAX_ORDER_LENGTH} identity
                    providers.
                  </Text>
                ) : (
                  addable.length > 0 && (
                    <div className="flex flex-wrap items-center gap-2">
                      <Select
                        value={adding}
                        onValueChange={setAdding}
                        disabled={save.isPending}
                      >
                        <SelectTrigger
                          className="w-64"
                          aria-label="Identity provider to add"
                        >
                          <SelectValue placeholder="Add an identity provider" />
                        </SelectTrigger>
                        <SelectContent>
                          {addable.map((candidate) => (
                            <SelectItem
                              key={candidate.key}
                              value={candidate.key}
                            >
                              {candidate.name}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <Button
                        variant="secondary"
                        size="sm"
                        disabled={adding === "" || save.isPending}
                        onClick={() => {
                          const candidate = addable.find(
                            (c) => c.key === adding,
                          );
                          if (!candidate) return;
                          setDraft([
                            ...order,
                            { ...candidate, available: true },
                          ]);
                          setAdding("");
                        }}
                      >
                        Add
                      </Button>
                    </div>
                  )
                ))}
            </>
          )}
          {(canEdit || canRestore) && !remoteSessionIssuerId && (
            <Text muted small>
              This order can be changed once a server reaches {serviceName}.
            </Text>
          )}
          {readOnlyReason && (
            <Text muted small>
              {readOnlyReason}
            </Text>
          )}
          <ApiErrorAlert error={conflictError ? null : save.error} />
        </SettingsSection.Body>
        {remoteSessionIssuerId && (canEdit || (canRestore && !isDefault)) && (
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              {editing
                ? "Takes effect for the next person who connects."
                : custom
                  ? "Go back to the default to let whatever signs each server’s people in vouch for them."
                  : undefined}
            </SettingsSection.FooterHint>
            <SettingsSection.FooterActions>
              {canRestore && !isDefault && (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={save.isPending}
                  onClick={() => submit([])}
                >
                  Use default
                </Button>
              )}
              {canEdit && !editing && (
                <Button
                  variant={custom ? "secondary" : "primary"}
                  size="sm"
                  disabled={save.isPending}
                  onClick={() => (custom ? startEditing(saved) : customize())}
                >
                  {custom ? "Edit order" : "Customize order"}
                </Button>
              )}
              {canEdit && editing && (
                <>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={save.isPending}
                    onClick={stopEditing}
                  >
                    Cancel
                  </Button>
                  <Button
                    variant="primary"
                    size="sm"
                    disabled={
                      save.isPending ||
                      changedUnderneath ||
                      !dirty ||
                      order.length === 0 ||
                      order.length > MAX_ORDER_LENGTH ||
                      unavailable.length > 0
                    }
                    onClick={() => submit(order)}
                  >
                    {save.isPending ? "Saving…" : "Save order"}
                  </Button>
                </>
              )}
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        )}
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function providerInput(entry: OrderCandidate): EMAProviderInput {
  return entry.kind === "speakeasy" || !entry.remoteSessionIssuerId
    ? { kind: "speakeasy" }
    : { kind: "external", remoteSessionIssuerId: entry.remoteSessionIssuerId };
}

function sameOrder(a: Entry[], b: Entry[]): boolean {
  return (
    a.length === b.length && a.every((entry, i) => entry.key === b[i]?.key)
  );
}
