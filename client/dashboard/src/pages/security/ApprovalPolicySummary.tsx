import { Alert, AlertDescription } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { JSX } from "react";

/** A require_approval policy holds MCP tool calls until a listed approver
 *  approves them. The detection editors cannot edit one without dropping its
 *  approvers, so it opens read-only here. */
export function ApprovalPolicySummary({
  policy,
}: {
  policy: RiskPolicy;
}): JSX.Element {
  const approvers = policy.approvalConfig?.approvers ?? [];
  return (
    <Stack gap={4}>
      <Alert variant="info" alignTop>
        <AlertDescription>
          This tool approval policy holds matching MCP tool calls until an
          approver approves them. It is read-only here.
        </AlertDescription>
      </Alert>
      <Card>
        <Stack gap={3}>
          <Text variant="subheading">{policy.name}</Text>
          <Stack gap={1}>
            <Text variant="small" className="text-muted-foreground">
              Approvers
            </Text>
            <div className="flex flex-wrap gap-1.5">
              {approvers.map((approver) => (
                <Badge key={approver} variant="neutral">
                  {approver}
                </Badge>
              ))}
            </div>
          </Stack>
        </Stack>
      </Card>
    </Stack>
  );
}
