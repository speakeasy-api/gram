import { useState } from "react";
import { ArrowDown, ArrowUp, X } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { SettingsSection } from "@/components/detail/settings-section";
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
import { invalidateAllOrganizationRemoteSessionIssuerEMAServices } from "@gram/client/react-query/organizationRemoteSessionIssuerEMAServices.js";
import { useSetOrganizationRemoteSessionIssuerEMAProvidersMutation } from "@gram/client/react-query/setOrganizationRemoteSessionIssuerEMAProviders.js";

import type { ProviderView } from "./emaServiceView";

/** One identity provider an admin can put in the order. */
export type OrderCandidate = {
  key: string;
  name: string;
  input: EMAProviderInput;
};

/**
 * Which identity providers are tried for a service, in what order. The saved
 * order applies to every server that reaches the service; with none saved,
 * each server tries its own identity provider first, then Speakeasy.
 */
export function EmaProviderOrderEditor({
  serviceName,
  remoteSessionIssuerId,
  current,
  isDefault,
  candidates,
  canEdit,
}: {
  serviceName: string;
  /** Any issuer record naming the service; the API keys the order by it. */
  remoteSessionIssuerId: string | undefined;
  current: ProviderView[];
  isDefault: boolean;
  candidates: OrderCandidate[];
  canEdit: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<OrderCandidate[] | null>(null);
  const [adding, setAdding] = useState("");
  const saved = isDefault
    ? []
    : current.flatMap((provider) => {
        const candidate = candidates.find((c) => c.key === provider.key);
        return candidate
          ? [candidate]
          : [
              {
                key: provider.key,
                name: provider.name,
                input: providerInput(provider),
              },
            ];
      });
  const order = draft ?? saved;
  const dirty = draft !== null && !sameOrder(draft, saved);
  const addable = candidates.filter(
    (candidate) => !order.some((entry) => entry.key === candidate.key),
  );

  const save = useSetOrganizationRemoteSessionIssuerEMAProvidersMutation({
    onSuccess: (result) => {
      void invalidateAllOrganizationRemoteSessionIssuerEMAServices(queryClient);
      setDraft(null);
      toast.success(
        result.isDefault
          ? `${serviceName} uses the default order`
          : `Saved the order for ${serviceName}`,
      );
    },
    onError: () => undefined,
  });
  const submit = (providers: EMAProviderInput[]) => {
    if (!remoteSessionIssuerId) return;
    save.mutate({
      request: {
        setEMAProvidersRequestBody: { remoteSessionIssuerId, providers },
      },
    });
  };
  const move = (index: number, by: -1 | 1) => {
    const next = [...order];
    const [entry] = next.splice(index, 1);
    if (!entry) return;
    next.splice(index + by, 0, entry);
    setDraft(next);
  };

  return (
    <SettingsSection id="order">
      <SettingsSection.Header>
        <SettingsSection.Title>Order</SettingsSection.Title>
        <SettingsSection.Description>
          Which identity provider vouches first when someone connects to{" "}
          {serviceName}, and which is tried next if that one can’t. Applies to
          every server that reaches {serviceName}.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {order.length === 0 ? (
            <Text small>
              Default: each server’s own identity provider first, then Speakeasy
              when it’s set up.
            </Text>
          ) : (
            <ol aria-label="Provider order" className="space-y-2">
              {order.map((entry, index) => (
                <li key={entry.key} className="flex items-center gap-3">
                  <Text muted small className="w-5 text-right tabular-nums">
                    {index + 1}
                  </Text>
                  <Text small className="min-w-0 flex-1 truncate font-medium">
                    {entry.name}
                  </Text>
                  {canEdit && (
                    <span className="flex items-center gap-1">
                      <Button
                        variant="tertiary"
                        size="sm"
                        aria-label={`Move ${entry.name} up`}
                        disabled={index === 0 || save.isPending}
                        onClick={() => move(index, -1)}
                      >
                        <ArrowUp aria-hidden="true" />
                      </Button>
                      <Button
                        variant="tertiary"
                        size="sm"
                        aria-label={`Move ${entry.name} down`}
                        disabled={index === order.length - 1 || save.isPending}
                        onClick={() => move(index, 1)}
                      >
                        <ArrowDown aria-hidden="true" />
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
                        <X aria-hidden="true" />
                      </Button>
                    </span>
                  )}
                </li>
              ))}
            </ol>
          )}
          {canEdit && addable.length > 0 && (
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
                    <SelectItem key={candidate.key} value={candidate.key}>
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
                  const candidate = addable.find((c) => c.key === adding);
                  if (!candidate) return;
                  setDraft([...order, candidate]);
                  setAdding("");
                }}
              >
                Add
              </Button>
            </div>
          )}
          {canEdit && !remoteSessionIssuerId && (
            <Text muted small>
              This order can be changed once a server reaches {serviceName}.
            </Text>
          )}
          {!canEdit && (
            <Text muted small>
              Only organization admins signed in directly can change the order.
            </Text>
          )}
          <ApiErrorAlert error={save.error} />
        </SettingsSection.Body>
        {canEdit && remoteSessionIssuerId && (dirty || !isDefault) && (
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              {dirty
                ? "Takes effect for the next person who connects."
                : "A saved order replaces the default for every server."}
            </SettingsSection.FooterHint>
            <SettingsSection.FooterActions>
              {!isDefault && (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={save.isPending}
                  onClick={() => submit([])}
                >
                  Use default
                </Button>
              )}
              {dirty && (
                <>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={save.isPending}
                    onClick={() => setDraft(null)}
                  >
                    Cancel
                  </Button>
                  <Button
                    variant="primary"
                    size="sm"
                    disabled={save.isPending || order.length === 0}
                    onClick={() => submit(order.map((entry) => entry.input))}
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

function providerInput(provider: ProviderView): EMAProviderInput {
  return provider.kind === "speakeasy"
    ? { kind: "speakeasy" }
    : {
        kind: "external",
        remoteSessionIssuerId: provider.key.replace(/^external-/, ""),
      };
}

function sameOrder(a: OrderCandidate[], b: OrderCandidate[]): boolean {
  return (
    a.length === b.length && a.every((entry, i) => entry.key === b[i]?.key)
  );
}
