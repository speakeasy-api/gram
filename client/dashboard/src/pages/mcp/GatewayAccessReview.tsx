import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Text } from "@/components/ui/Text";
import { useCallback, useState } from "react";
import { MCPAssignments } from "../access/MCPAssignments";

export function GatewayAccessReview({
  isPending,
  onEnable,
  onCancel,
}: {
  isPending: boolean;
  onEnable: () => void;
  onCancel: () => void;
}): JSX.Element {
  const [ready, setReady] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);
  const onReady = useCallback((value: boolean) => {
    setReady(value);
    if (!value) setAcknowledged(false);
  }, []);
  return (
    <section className="mb-8 space-y-5" aria-label="Review gateway access">
      <div>
        <Text variant="subheading">Review access before enabling</Text>
        <Text small muted>
          Each person gets every MCP server they can access across all projects,
          including access inherited from broad roles. Remove assignments people
          no longer need, then approve enabling Organization Gateway.
        </Text>
      </div>
      <div className="border-border bg-card space-y-4 border p-4">
        <label className="flex items-start gap-3 text-sm">
          <Checkbox
            checked={ready && acknowledged}
            disabled={!ready || isPending}
            onCheckedChange={(value) => setAcknowledged(value === true)}
          />
          I have reviewed MCP assignments across the organization and approve
          making each person’s assigned servers available through Organization
          Gateway.
        </label>
        <div className="flex gap-3">
          <Button
            disabled={!ready || !acknowledged || isPending}
            onClick={onEnable}
          >
            {isPending ? "Enabling…" : "Enable Organization Gateway"}
          </Button>
          <Button variant="secondary" disabled={isPending} onClick={onCancel}>
            Cancel
          </Button>
        </div>
      </div>
      <MCPAssignments onReady={onReady} />
    </section>
  );
}
