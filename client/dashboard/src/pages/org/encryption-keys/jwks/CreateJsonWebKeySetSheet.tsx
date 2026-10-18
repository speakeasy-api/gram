import { Button } from "@/components/ui/Button";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { useOrgRoutes } from "@/routes";
import type { JSONWebKeySet } from "@gram/client/models/components/jsonwebkeyset.js";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { KeySetSetupSteps } from "./KeySetSetupSteps";

// CreateJsonWebKeySetSheet sets up a signing key set from a KMS key, walking
// through the cloud side first: a service account Speakeasy impersonates and
// its grant on the key ring. Creating the set publishes the key as its active
// signing key, so the set is ready for verifiers as soon as the sheet closes.
export function CreateJsonWebKeySetSheet({
  open,
  onOpenChange,
  isCurrentOrganization,
  onCreated,
  defaultName = "",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  isCurrentOrganization: () => boolean;
  /**
   * Hands the new set back instead of going to its page, for a flow that
   * opened the sheet to pick the set it creates.
   */
  onCreated?: (set: JSONWebKeySet) => void;
  defaultName?: string;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  // Remounts the steps on every open so a prior draft never leaks into a new
  // setup; what was connected or registered is offered again from the lists.
  const [session, setSession] = useState(0);
  useEffect(() => {
    if (open) setSession((current) => current + 1);
  }, [open]);

  const handleCreated = (created: JSONWebKeySet) => {
    toast.success("Signing key set created");
    onOpenChange(false);
    if (onCreated) onCreated(created);
    else orgRoutes.signingKeySets.setDetail.keys.goTo(created.id);
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[680px] flex-col sm:max-w-[680px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            New Signing Key Set
          </SheetTitle>
        </SheetHeader>

        <KeySetSetupSteps
          key={session}
          defaultName={defaultName || "Production signing keys"}
          onCreated={handleCreated}
          isCurrentOrganization={isCurrentOrganization}
          footer={(action) => (
            <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
              <Button
                variant="secondary"
                disabled={action.pending}
                onClick={() => onOpenChange(false)}
              >
                <Button.Text>Cancel</Button.Text>
              </Button>
              <Button
                variant="primary"
                disabled={action.disabled}
                onClick={action.run}
              >
                <Button.Text>{action.label}</Button.Text>
              </Button>
            </SheetFooter>
          )}
        />
      </SheetContent>
    </Sheet>
  );
}
