import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useSdkClient } from "@/contexts/Sdk";
import type { GramGatewayToolsetReview } from "@gram/client/models/components/gramgatewaytoolsetreview.js";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Checkbox } from "@/components/ui/Checkbox";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

export type FrozenGatewayConnection = {
  review: GramGatewayToolsetReview;
  names: string[];
};

export function GatewayFrozenToolset({
  gatewayId,
  approved,
  hasUnappliedFreeze = false,
  pending,
  onApply,
}: {
  gatewayId: string;
  approved: FrozenGatewayConnection | undefined;
  hasUnappliedFreeze?: boolean;
  pending: boolean;
  onApply: (value: FrozenGatewayConnection | undefined) => void;
}): JSX.Element | null {
  const organization = useOrganization();
  const { data: features } = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const enabled = features?.gatewayFrozenToolsetsEnabled === true;
  const [review, setReview] = useState<GramGatewayToolsetReview>();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  useEffect(() => {
    setReview(undefined);
  }, [approved]);
  const client = useSdkClient();
  const preview = useMutation({
    mutationFn: () =>
      client.userSessions.previewGatewayToolset({ metaMcpServerId: gatewayId }),
    throwOnError: false,
    onSuccess: (next) => {
      setReview(next);
      const previous = new Map(
        approved?.review.tools
          .filter((tool) => approved.names.includes(tool.name))
          .map((tool) => [tool.name, tool.fingerprint]),
      );
      setSelected(
        new Set(
          next.tools
            .filter(
              (tool) =>
                !approved || previous.get(tool.name) === tool.fingerprint,
            )
            .map((tool) => tool.name),
        ),
      );
    },
  });
  if (!enabled && !approved && !hasUnappliedFreeze) return null;
  const previous = new Map(
    approved?.review.tools
      .filter((tool) => approved.names.includes(tool.name))
      .map((tool) => [tool.name, tool]),
  );
  const removed = review
    ? [...previous.keys()].filter(
        (name) => !review.tools.some((tool) => tool.name === name),
      )
    : [];
  return (
    <section className="space-y-4 border p-4" aria-label="Frozen toolset">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="space-y-1">
          <Text className="font-medium">Toolset</Text>
          <Text muted className="text-sm">
            {approved
              ? `${approved.names.length} ${approved.names.length === 1 ? "tool" : "tools"} frozen for this connection`
              : hasUnappliedFreeze
                ? "Freeze not applied to this connection"
                : "Live tools — definitions can change"}
          </Text>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="secondary"
            disabled={!enabled || preview.isPending || pending}
            onClick={() => {
              setReview(undefined);
              preview.mutate();
            }}
          >
            <Button.Text>
              {preview.isPending
                ? "Loading tools…"
                : approved
                  ? "Review changes"
                  : "Review and freeze"}
            </Button.Text>
          </Button>
          {(approved || hasUnappliedFreeze) && (
            <Button
              variant="tertiary"
              disabled={pending}
              onClick={() => onApply(undefined)}
            >
              <Button.Text>Use live tools and reconnect</Button.Text>
            </Button>
          )}
        </div>
      </div>
      <Text muted className="text-sm">
        Optional. Changed and new tools stay unavailable until approved. Access
        can still be revoked. Definitions do not prove unchanged upstream
        behavior.
      </Text>
      {preview.isError && (
        <Text role="alert" destructive>
          {preview.error.message}
        </Text>
      )}
      {review && (
        <div className="space-y-3">
          {removed.length > 0 && (
            <Text className="text-sm">
              No longer available: {removed.join(", ")}
            </Text>
          )}
          {review.tools.length === 0 && (
            <Text muted>
              No tools are available. Freezing this list permits no tools.
            </Text>
          )}
          <div className="max-h-96 space-y-2 overflow-auto">
            {review.tools.map((tool) => {
              const old = previous.get(tool.name);
              const changed = !!old && old.fingerprint !== tool.fingerprint;
              return (
                <div key={tool.name} className="space-y-2 border p-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <Checkbox
                      id={`freeze-${tool.name}`}
                      checked={selected.has(tool.name)}
                      onCheckedChange={(checked) =>
                        setSelected((current) => {
                          const next = new Set(current);
                          if (checked === true) next.add(tool.name);
                          else next.delete(tool.name);
                          return next;
                        })
                      }
                    />
                    <label
                      htmlFor={`freeze-${tool.name}`}
                      className="break-all font-mono text-sm"
                    >
                      {tool.name}
                    </label>
                    {changed && (
                      <Badge variant="warning">
                        <Badge.Text>Changed since approval</Badge.Text>
                      </Badge>
                    )}
                    {approved && !old && (
                      <Badge>
                        <Badge.Text>Not approved</Badge.Text>
                      </Badge>
                    )}
                  </div>
                  <details>
                    <summary className="cursor-pointer text-sm">
                      {changed ? "Compare definitions" : "Tool definition"}
                    </summary>
                    {changed && (
                      <Text muted className="text-xs">
                        A matching definition with a changed fingerprint means
                        its routing identity changed.
                      </Text>
                    )}
                    <div className={changed ? "grid gap-3 md:grid-cols-2" : ""}>
                      {changed && (
                        <div>
                          <Text className="text-xs">Previously approved</Text>
                          <pre className="overflow-auto text-xs">
                            {old.definition}
                          </pre>
                        </div>
                      )}
                      <div>
                        <Text className="text-xs">Current</Text>
                        <pre className="overflow-auto text-xs">
                          {tool.definition}
                        </pre>
                      </div>
                    </div>
                  </details>
                </div>
              );
            })}
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button
              disabled={!enabled || pending || preview.isPending}
              onClick={() => onApply({ review, names: [...selected] })}
            >
              <Button.Text>
                Freeze {selected.size} {selected.size === 1 ? "tool" : "tools"}{" "}
                and reconnect
              </Button.Text>
            </Button>
            <Button variant="tertiary" onClick={() => setReview(undefined)}>
              <Button.Text>Cancel</Button.Text>
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}
