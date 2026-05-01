import { Button, type Column } from "@speakeasy-api/moonshine";
import { useState } from "react";
import type { AuthzChallenge } from "./ChallengesTab";
import { CreateRoleDialog } from "./CreateRoleDialog";
import { GrantDrawer } from "./GrantDrawer";

export function useGrantFlow() {
  const [grantChallenge, setGrantChallenge] = useState<AuthzChallenge | null>(
    null,
  );
  const [isCreateOpen, setIsCreateOpen] = useState(false);

  const actionsColumn: Column<AuthzChallenge> = {
    key: "actions",
    header: "",
    width: "100px",
    render: (row) =>
      row.outcome === "deny" && !row.resolvedAt ? (
        <Button
          variant="primary"
          size="sm"
          onClick={() => setGrantChallenge(row)}
        >
          <Button.Text>Grant</Button.Text>
        </Button>
      ) : null,
  };

  const grantFlowPortals = (
    <>
      <GrantDrawer
        open={!!grantChallenge}
        onOpenChange={(isOpen) => {
          if (!isOpen) setGrantChallenge(null);
        }}
        challenge={grantChallenge}
        onCreateNew={() => setIsCreateOpen(true)}
      />

      <CreateRoleDialog
        open={isCreateOpen}
        onOpenChange={(isOpen) => {
          if (!isOpen) setIsCreateOpen(false);
        }}
        editingRole={null}
      />
    </>
  );

  return { actionsColumn, grantFlowPortals };
}
