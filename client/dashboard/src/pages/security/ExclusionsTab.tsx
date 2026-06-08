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
import type { ReactNode } from "react";
import { Ellipsis, Plus } from "lucide-react";
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
          <ExclusionActionsMenu
            onEdit={() => onSheetChange({ mode: "edit", exclusion })}
            onDelete={() =>
              deleteMutation.mutate({ request: { id: exclusion.id } })
            }
          />
        </div>
      ),
    },
  ];

  let body: ReactNode;
  if (isLoading) {
    body = <Type className="text-muted-foreground">Loading exclusions…</Type>;
  } else if (exclusions.length === 0) {
    body = (
      <ExclusionsEmptyState
        onCreate={() => onSheetChange({ mode: "create" })}
      />
    );
  } else {
    body = (
      <Table
        columns={columns}
        data={exclusions}
        rowKey={(exclusion) => exclusion.id}
        onRowClick={(exclusion) => onSheetChange({ mode: "edit", exclusion })}
      />
    );
  }

  return (
    <>
      {body}
      <ExclusionSheet
        state={sheet}
        onOpenChange={(open) => {
          if (!open) onSheetChange(null);
        }}
      />
    </>
  );
}

function ExclusionActionsMenu({
  onEdit,
  onDelete,
}: {
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <DropdownMenu modal={false}>
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
          onSelect={() => setTimeout(onEdit, 0)}
        >
          Edit
        </DropdownMenuItem>
        <DropdownMenuItem
          className="text-destructive focus:text-destructive cursor-pointer"
          onSelect={() => setTimeout(onDelete, 0)}
        >
          Delete
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ExclusionsEmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="bg-background flex h-[360px] w-full flex-col items-center justify-center gap-4 rounded-xl border">
      <div className="space-y-1 text-center">
        <Type className="font-medium">No exclusions yet</Type>
        <Type small muted>
          Create an exclusion to suppress false-positive findings.
        </Type>
      </div>
      <Button onClick={onCreate}>
        <Button.LeftIcon>
          <Plus className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Create exclusion</Button.Text>
      </Button>
    </div>
  );
}
