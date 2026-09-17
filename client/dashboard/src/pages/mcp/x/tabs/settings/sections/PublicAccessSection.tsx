import { SettingsSection } from "@/components/detail/settings-section";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { useUpdateTunneledMcpServerMutation } from "@gram/client/react-query/updateTunneledMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { invalidateTunneledMcpSourceViews } from "./sourceInvalidation";

export const MCP_PUBLIC_ACCESS_SECTION_ID = "public-access";

const PUBLIC_ACCESS_CONFIRM_PHRASE = "ALLOW PUBLIC ACCESS";

type PendingChange = "enable" | "disable" | null;

function PendingIcon({ pending }: { pending: boolean }) {
  if (!pending) return null;
  return (
    <Button.LeftIcon>
      <Loader2 className="size-4 animate-spin" />
    </Button.LeftIcon>
  );
}

// Public visibility is a double opt-in: the tunnel source owner consents here,
// and only then can the MCP server fronting it be set to public. Ported from
// the retired tunneled source page.
export function PublicAccessSection({
  tunneledMcpServer,
}: {
  tunneledMcpServer: TunneledMcpServer;
}): JSX.Element {
  const update = useUpdateTunneledMcpServerMutation();
  const queryClient = useQueryClient();
  const [pending, setPending] = useState<PendingChange>(null);
  const [confirmPhrase, setConfirmPhrase] = useState("");

  const allowPublic = tunneledMcpServer.allowPublic;

  const closeDialog = () => {
    setPending(null);
    setConfirmPhrase("");
    update.reset();
  };

  const applyAllowPublic = async (next: boolean) => {
    try {
      await update.mutateAsync({
        request: {
          updateTunneledMcpServerForm: {
            id: tunneledMcpServer.id,
            allowPublic: next,
          },
        },
      });
      await invalidateTunneledMcpSourceViews(queryClient);
      toast.success(
        next
          ? "Public anonymous access enabled"
          : "Public anonymous access disabled",
      );
      closeDialog();
    } catch (error) {
      // Keep the dialog open on failure so the user can retry; the error
      // Alert inside it and the toast already surface the reason.
      const message =
        error instanceof Error
          ? error.message
          : "Failed to update public access";
      toast.error(message);
    }
  };

  const enableArmed = confirmPhrase.trim() === PUBLIC_ACCESS_CONFIRM_PHRASE;

  return (
    <SettingsSection id={MCP_PUBLIC_ACCESS_SECTION_ID}>
      <SettingsSection.Header>
        <div className="flex items-center gap-2">
          <SettingsSection.Title>Public Access</SettingsSection.Title>
          <ReleaseStageBadge stage="preview" />
        </div>
        <SettingsSection.Description>
          When enabled, this MCP server can be set to public visibility, serving
          fully anonymous callers with no login. Anyone who can reach the
          endpoint URL can call every tool the tunneled source exposes. Leave
          this off unless the upstream MCP server is safe to expose to the
          public internet.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {allowPublic ? (
            <Alert variant="warning" dismissible={false}>
              Public anonymous access is enabled. This server may serve
              unauthenticated callers once its visibility is set to public.
            </Alert>
          ) : (
            <Text muted small>
              Public anonymous access is off. The visibility picker offers only
              Disabled and Private until it is enabled here.
            </Text>
          )}
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            {allowPublic ? "Enabled" : "Disabled"}
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope scope="mcp:write" level="component">
              <Button
                variant="secondary"
                size="md"
                disabled={update.isPending}
                onClick={() => setPending(allowPublic ? "disable" : "enable")}
              >
                <Button.Text>
                  {allowPublic
                    ? "Disable public access"
                    : "Enable public access"}
                </Button.Text>
              </Button>
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>

      <Dialog
        open={pending === "enable"}
        onOpenChange={(open) => {
          if (!open) closeDialog();
        }}
      >
        <Dialog.Content className="max-w-xl!">
          <Dialog.Header>
            <Dialog.Title>Enable public anonymous access?</Dialog.Title>
            <Dialog.Description>
              This lets anyone on the internet call this source's tools with no
              authentication once the MCP server fronting it is set to public
              visibility.
            </Dialog.Description>
          </Dialog.Header>
          <Alert variant="warning" dismissible={false}>
            Only enable this for MCP servers that are safe to expose publicly.
            Every tool, resource, and prompt becomes reachable without a login.
          </Alert>
          <Stack gap={2}>
            <Text small muted>
              Type{" "}
              <span className="font-mono font-medium">
                {PUBLIC_ACCESS_CONFIRM_PHRASE}
              </span>{" "}
              to confirm.
            </Text>
            <Input
              value={confirmPhrase}
              onChange={(value) => setConfirmPhrase(value)}
              placeholder={PUBLIC_ACCESS_CONFIRM_PHRASE}
            />
            {update.isError && (
              <Alert variant="error" dismissible={false}>
                {update.error.message}
              </Alert>
            )}
          </Stack>
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={closeDialog}
              disabled={update.isPending}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              disabled={!enableArmed || update.isPending}
              onClick={() => void applyAllowPublic(true)}
            >
              <PendingIcon pending={update.isPending} />
              <Button.Text>
                {update.isPending ? "Enabling" : "Enable public access"}
              </Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>

      <Dialog
        open={pending === "disable"}
        onOpenChange={(open) => {
          if (!open) closeDialog();
        }}
      >
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Disable public access?</Dialog.Title>
            <Dialog.Description>
              Anonymous callers will be turned away. If this server is currently
              public, set its visibility to private or disabled as well.
            </Dialog.Description>
          </Dialog.Header>
          {update.isError && (
            <Alert variant="error" dismissible={false}>
              {update.error.message}
            </Alert>
          )}
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={closeDialog}
              disabled={update.isPending}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="primary"
              disabled={update.isPending}
              onClick={() => void applyAllowPublic(false)}
            >
              <PendingIcon pending={update.isPending} />
              <Button.Text>
                {update.isPending ? "Disabling" : "Disable public access"}
              </Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </SettingsSection>
  );
}
