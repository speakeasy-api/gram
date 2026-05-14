import { Dialog } from "@/components/ui/dialog";
import { Toolset } from "@/lib/toolTypes";
import { Stack } from "@speakeasy-api/moonshine";

// WireUserSessionIssuerModal walks an admin through migrating an MCP toolset
// from the legacy OAuth Proxy paradigm to the user-session / remote-session
// world: create a user_session_issuer for the project, register or clone a
// remote_session_issuer + remote_session_client pair, and reuse the existing
// upstream client_id so already-registered redirect URIs keep working.
//
// This file is the modal's scaffold. The workflow hooks that drive each step
// and the step-by-step UI land in follow-up commits.
export function WireUserSessionIssuerModal({
  isOpen,
  onClose,
  toolset,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolset: Toolset;
}) {
  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-h-[90vh] max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>Wire User Session Issuer</Dialog.Title>
          <Dialog.Description>
            Port the OAuth Proxy configuration on{" "}
            <span className="font-medium">{toolset.name ?? toolset.slug}</span>{" "}
            to a user session issuer paired with a remote session issuer and
            client.
          </Dialog.Description>
        </Dialog.Header>
        <Stack gap={4} className="text-muted-foreground py-4 text-sm">
          The migration workflow is not yet wired up. Subsequent commits add the
          step-by-step UI here.
        </Stack>
      </Dialog.Content>
    </Dialog>
  );
}
