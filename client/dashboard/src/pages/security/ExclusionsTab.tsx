import { Badge } from "@/components/ui/Badge";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import type { Action } from "@/components/ui/MoreActions";
import { Text } from "@/components/ui/Text";
import { Switch } from "@/components/ui/Switch";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { type Column, Table } from "@/components/ui/Table";
import { useRiskDeleteExclusionMutation } from "@gram/client/react-query/riskDeleteExclusion.js";
import {
  invalidateAllRiskListExclusions,
  useRiskListExclusions,
} from "@gram/client/react-query/riskListExclusions.js";
import { useRiskUpdateExclusionMutation } from "@gram/client/react-query/riskUpdateExclusion.js";
import type { RiskExclusion } from "@gram/client/models/components/riskexclusion.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { useQueryClient } from "@tanstack/react-query";
import { format } from "date-fns";
import type { JSX, ReactNode } from "react";
import { Ellipsis, Plus } from "lucide-react";
import { serializeExclusionExpression } from "./exclusion-expression";
import { ExclusionSheet, type ExclusionSheetState } from "./exclusion-sheet";
import { BuiltinLibrary } from "./builtin-library";

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
}: ExclusionsTabProps): JSX.Element {
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

  const exclusionActions = (exclusion: RiskExclusion): Action[] => [
    {
      label: "Edit",
      onClick: () => onSheetChange({ mode: "edit", exclusion }),
    },
    {
      label: "Delete",
      destructive: true,
      onClick: () => {
        deleteMutation.mutate({ request: { id: exclusion.id } });
      },
    },
  ];

  const columns: Column<RiskExclusion>[] = [
    {
      key: "criteria",
      header: "Criteria",
      width: "2fr",
      render: (exclusion) => (
        <Text className="truncate font-mono text-xs" mono>
          {serializeExclusionExpression(exclusion)}
        </Text>
      ),
    },
    {
      key: "type",
      header: "Type",
      width: "0.8fr",
      render: (exclusion) => (
        <Badge variant="neutral">{exclusion.matchType}</Badge>
      ),
    },
    {
      key: "scope",
      header: "Scope",
      width: "1fr",
      render: (exclusion) => {
        const name = policyName(exclusion.riskPolicyId);
        if (!name) return <Badge variant="warning">Global</Badge>;
        return <Badge variant="neutral">{name}</Badge>;
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
        <Text className="text-muted-foreground" small>
          {format(exclusion.createdAt, "MMM d, yyyy")}
        </Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "0.3fr",
      render: (exclusion) => (
        <div onClick={(e) => e.stopPropagation()}>
          <ExclusionActionsMenu actions={exclusionActions(exclusion)} />
        </div>
      ),
    },
  ];

  let body: ReactNode;
  if (isLoading) {
    body = <Text className="text-muted-foreground">Loading exclusions…</Text>;
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
        renderRow={(exclusion, rowElement) => (
          <TableRowContextMenu
            key={exclusion.id}
            actions={exclusionActions(exclusion)}
          >
            {rowElement}
          </TableRowContextMenu>
        )}
      />
    );
  }

  return (
    <>
      <BuiltinLibrary />
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

function ExclusionActionsMenu({ actions }: { actions: Action[] }): JSX.Element {
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button variant="tertiary" size="sm" aria-label="Exclusion actions">
          <Button.Icon>
            <Ellipsis className="h-4 w-4" />
          </Button.Icon>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {actions.map((action) => (
          <DropdownMenuItem
            key={action.label}
            disabled={action.disabled}
            className={
              action.destructive
                ? "text-destructive focus:text-destructive cursor-pointer"
                : "cursor-pointer"
            }
            onSelect={() => {
              setTimeout(action.onClick, 0);
            }}
          >
            {action.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ExclusionsEmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="bg-background flex h-[360px] w-full flex-col items-center justify-center gap-4 border">
      <div className="space-y-1 text-center">
        <Text className="font-medium">No exclusions yet</Text>
        <Text small muted>
          Create an exclusion to suppress false-positive findings.
        </Text>
      </div>
      <Button onClick={onCreate}>
        <Button.LeftIcon>
          <Plus className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Set up exclusion rule</Button.Text>
      </Button>
    </div>
  );
}
