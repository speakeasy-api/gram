import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { Button } from "@speakeasy-api/moonshine";
import { Loader2 } from "lucide-react";

import { WizardContext } from "./machine";

export function AutoRegisterChoice() {
  const send = WizardContext.useActorRef().send;
  const isLoading = WizardContext.useSelector(
    (s) =>
      s.matches({ proxy: "registering" }) ||
      (s.matches({ proxy: "submitting" }) && s.context.autoRegistering),
  );

  if (isLoading) {
    return (
      <div className="flex flex-col items-center justify-center gap-4 py-12">
        <Loader2 className="text-muted-foreground h-12 w-12 animate-spin" />
        <Type className="text-center text-lg font-medium">
          Fetching credentials...
        </Type>
        <Type muted small className="max-w-md text-center">
          Registering Gram with the upstream OAuth provider and storing the
          returned credentials.
        </Type>
      </div>
    );
  }

  return (
    <>
      <div className="flex flex-col gap-4 pt-2 pb-4">
        <Type>Automatic Client Registration</Type>
        <Type muted small>
          Speakeasy can automatically fetch the required credentials (Client ID
          and Client Secret) from the OAuth server.
        </Type>
        <Type muted small>
          How would you like to proceed?
        </Type>
      </div>

      <Dialog.Footer className="flex justify-between">
        <Button variant="secondary" onClick={() => send({ type: "BACK" })}>
          Back
        </Button>
        <div className="ml-auto flex gap-2">
          <Button
            variant="secondary"
            onClick={() => send({ type: "MANUAL_CREDENTIALS" })}
          >
            Supply my Own
          </Button>
          <Button onClick={() => send({ type: "AUTO_REGISTER" })}>Fetch</Button>
        </div>
      </Dialog.Footer>
    </>
  );
}
