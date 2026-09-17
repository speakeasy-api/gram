import { SettingsSection } from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import {
  useRotateTunneledMcpServerKey,
  type RotateTunneledMcpServerKeyData,
} from "@/pages/sources/tunneled-mcp/hooks";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { KeyRound, Loader2, RotateCcw } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

export const MCP_TUNNEL_KEY_SECTION_ID = "tunnel-key";

function RotatedKeyDialogBody({
  rotatedKey,
  onClose,
}: {
  rotatedKey: RotateTunneledMcpServerKeyData;
  onClose: () => void;
}) {
  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Tunnel Key Rotated</Dialog.Title>
        <Dialog.Description>
          Copy the new key now. It will not be shown again.
        </Dialog.Description>
      </Dialog.Header>
      <Alert variant="warning" dismissible={false}>
        Restart tunnel agents with the new key to reconnect this source.
      </Alert>
      <div className="bg-muted flex items-center gap-2 p-3">
        <code className="min-w-0 flex-1 text-sm break-all">
          {rotatedKey.tunnelKey}
        </code>
        <CopyButton
          text={rotatedKey.tunnelKey}
          size="sm"
          tooltip="Copy tunnel key"
        />
      </div>
      <div className="flex items-center gap-2">
        <KeyRound className="text-muted-foreground h-4 w-4" />
        <Text small muted>
          Prefix: {rotatedKey.tunneledMcpServer.keyPrefix}
        </Text>
      </div>
      <Dialog.Footer>
        <Button onClick={onClose}>
          <Button.Text>Close</Button.Text>
        </Button>
      </Dialog.Footer>
    </>
  );
}

// Ported from the retired tunneled source page: the key is issued to the
// source, so its rotation is managed from the MCP server that fronts it.
export function TunnelKeySection({
  tunneledMcpServer,
}: {
  tunneledMcpServer: TunneledMcpServer;
}): JSX.Element {
  const [rotateDialogOpen, setRotateDialogOpen] = useState(false);
  const [rotatedKey, setRotatedKey] =
    useState<RotateTunneledMcpServerKeyData>();
  const rotate = useRotateTunneledMcpServerKey();

  const handleOpenChange = (open: boolean) => {
    setRotateDialogOpen(open);
    if (!open) {
      setRotatedKey(undefined);
      rotate.reset();
    }
  };

  const handleRotate = async () => {
    try {
      const result = await rotate.mutateAsync({
        tunneledMcpServerId: tunneledMcpServer.id,
      });
      setRotatedKey(result);
      toast.success("Tunnel key rotated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to rotate tunnel key";
      toast.error(message);
    }
  };

  return (
    <SettingsSection id={MCP_TUNNEL_KEY_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Tunnel Key</SettingsSection.Title>
        <SettingsSection.Description>
          Tunnel agents authenticate to this source with its key. Rotation
          replaces the key; running agents must be restarted with the
          replacement.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <div className="flex items-center gap-2">
            <KeyRound className="text-muted-foreground h-4 w-4" />
            <Text small muted>
              Current key prefix
            </Text>
            <Text small mono>
              {tunneledMcpServer.keyPrefix}
            </Text>
          </div>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            The full key was shown once when it was issued.
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope scope="mcp:write" level="component">
              <Button
                variant="secondary"
                size="md"
                onClick={() => setRotateDialogOpen(true)}
              >
                <Button.LeftIcon>
                  <RotateCcw className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Rotate key</Button.Text>
              </Button>
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>

      <Dialog open={rotateDialogOpen} onOpenChange={handleOpenChange}>
        <Dialog.Content className="max-w-xl!">
          {rotatedKey ? (
            <RotatedKeyDialogBody
              rotatedKey={rotatedKey}
              onClose={() => handleOpenChange(false)}
            />
          ) : (
            <>
              <Dialog.Header>
                <Dialog.Title>Rotate Tunnel Key</Dialog.Title>
                <Dialog.Description>
                  The current key will stop working for new tunnel connections.
                </Dialog.Description>
              </Dialog.Header>
              <Alert variant="warning" dismissible={false}>
                Running agents using the old key will be disconnected shortly
                and must be restarted with the replacement key.
              </Alert>
              {rotate.isError && (
                <Alert variant="error" dismissible={false}>
                  {rotate.error.message}
                </Alert>
              )}
              <Dialog.Footer>
                <Button
                  variant="secondary"
                  onClick={() => handleOpenChange(false)}
                  disabled={rotate.isPending}
                >
                  <Button.Text>Cancel</Button.Text>
                </Button>
                <Button
                  variant="destructive-primary"
                  onClick={() => void handleRotate()}
                  disabled={rotate.isPending}
                >
                  {rotate.isPending ? (
                    <Button.LeftIcon>
                      <Loader2 className="size-4 animate-spin" />
                    </Button.LeftIcon>
                  ) : null}
                  <Button.Text>
                    {rotate.isPending ? "Rotating" : "Rotate"}
                  </Button.Text>
                </Button>
              </Dialog.Footer>
            </>
          )}
        </Dialog.Content>
      </Dialog>
    </SettingsSection>
  );
}
