import { useState } from "react";

import { Button } from "@/components/ui/Button";
import { RegisterClientSheet } from "./RegisterClientSheet";
import { useCanManageIssuerClients } from "./useCanManageIssuerClients";

/**
 * Opens the register sheet for one MCP Authentication Provider. Hidden from
 * anyone who cannot register clients against it, as other write controls are.
 */
export function RegisterClientAction({
  issuerId,
}: {
  issuerId: string;
}): JSX.Element | null {
  const canManage = useCanManageIssuerClients(issuerId);
  const [open, setOpen] = useState(false);
  if (!canManage) return null;

  return (
    <>
      <Button size="sm" icon="plus" onClick={() => setOpen(true)}>
        <Button.Text>Register OAuth client</Button.Text>
      </Button>
      <RegisterClientSheet
        issuerId={issuerId}
        open={open}
        onOpenChange={setOpen}
      />
    </>
  );
}
