import { Badge } from "@/components/ui/badge";
import { Type } from "@/components/ui/type";
import { Switch } from "@/components/ui/switch";
import {
  Button,
  type Column,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Table,
} from "@speakeasy-api/moonshine";
import {
  invalidateAllRiskListExclusions,
  useRiskDeleteExclusionMutation,
  useRiskListExclusions,
  useRiskUpdateExclusionMutation,
} from "@gram/client/react-query/index.js";
import type { RiskExclusion } from "@gram/client/models/components/riskexclusion.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { useQueryClient } from "@tanstack/react-query";
import { format } from "date-fns";
import { Ellipsis } from "lucide-react";
import { serializeExclusionExpression } from "./exclusion-expression";
import { ExclusionSheet, type ExclusionSheetState } from "./exclusion-sheet";

export type { ExclusionSheetState } from "./exclusion-sheet";

interface ExclusionsTabProps {
  policies: RiskPolicy[];
  sheet: ExclusionSheetState | null;
  onSheetChange: (sheet: ExclusionSheetState | null) => void;
}

export function ExclusionsTab({
  policies,
  sheet,
  onSheetChange,
}: ExclusionsTabProps) {
  const queryClient = useQueryClient();
  const { data, isLoading } = useRiskListExclusions();
  const exclusions = data?.exclusions ?? [];

  const policyName = (id: string | undefined): string | null => {
    if (!id) return null;
    return policies.find((p) => p.id === id)?.name ?? "Unknown policy";
  };

  const invalidate = () => invalidateAllRiskListExclusions(queryClient);

  const updateMutation = useRiskUpdateExclusionMutation({
    onSuccess: invalidate,
  });
  const deleteMutation = useRiskDeleteExclusionMutation({
    onSuccess: invalidate,
  });

  const handleToggle = (exclusion: RiskExclusion, enabled: boolean) => {
    updateMutation.mutate({
      request: {
        updateRiskExclusionRequestBody: {
          id: exclusion.id,
          matchType: exclusion.matchType,
          matchValue: exclusion.matchValue,
          ruleIdFilter: exclusion.ruleIdFilter,
          sourceFilter: exclusion.sourceFilter,
          riskPolicyId: exclusion.riskPolicyId,
          enabled,
        },
      },
    });
  };

  const columns: Column<RiskExclusion>[] = [
    {
      key: "criteria",
      header: "Criteria",
      width: "2fr",
      render: (exclusion) => (
        <Type className="truncate font-mono text-xs" mono>
          {serializeExclusionExpression(exclusion)}
        </Type>
      ),
    },
    {
      key: "type",
      header: "Type",
      width: "0.8fr",
      render: (exclusion) => (
        <Badge variant="secondary">{exclusion.matchType}</Badge>
      ),
    },
    {
      key: "scope",
      header: "Scope",
      width: "1fr",
      render: (exclusion) => {
        const name = policyName(exclusion.riskPolicyId);
        if (!name) return <Badge variant="warning">Global</Badge>;
        return <Badge variant="secondary">{name}</Badge>;
      },
    },
    {
      key: "enabled",
      header: "Status",
      width: "0.5fr",
      render: (exclusion) => (
        <div onClick={(e) => e.stopPropagation()}>
          <Switch
            checked={exclusion.enabled}
            onCheckedChange={(checked) => handleToggle(exclusion, checked)}
          />
        </div>
      ),
    },
    {
      key: "created",
      header: "Created",
      width: "0.9fr",
      render: (exclusion) => (
        <Type className="text-muted-foreground" small>
          {format(exclusion.createdAt, "MMM d, yyyy")}
        </Type>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "0.3fr",
      render: (exclusion) => (
        <div onClick={(e) => e.stopPropagation()}>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="tertiary" size="sm">
                <Button.Icon>
                  <Ellipsis className="h-4 w-4" />
                </Button.Icon>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                className="cursor-pointer"
                onSelect={() => onSheetChange({ mode: "edit", exclusion })}
              >
                Edit
              </DropdownMenuItem>
              <DropdownMenuItem
                className="text-destructive focus:text-destructive cursor-pointer"
                onSelect={() =>
                  deleteMutation.mutate({ request: { id: exclusion.id } })
                }
              >
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      ),
    },
  ];

  return (
    <>
      {isLoading ? (
        <Type className="text-muted-foreground">Loading exclusions…</Type>
      ) : (
        <Table
          columns={columns}
          data={exclusions}
          rowKey={(exclusion) => exclusion.id}
          onRowClick={(exclusion) => onSheetChange({ mode: "edit", exclusion })}
          noResultsMessage={
            <Type className="text-muted-foreground">
              No exclusions yet. Create one to suppress false-positive findings.
            </Type>
          }
        />
      )}

      <ExclusionSheet
        state={sheet}
        onOpenChange={(open) => {
          if (!open) onSheetChange(null);
        }}
      />
    </>
  );
}
