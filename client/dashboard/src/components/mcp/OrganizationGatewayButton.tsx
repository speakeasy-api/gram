import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { useOrganizationGateway } from "@gram/client/react-query/organizationGateway.js";
import { Network } from "lucide-react";

/**
 * Offers the organization gateway: one URL that connects an AI client to every
 * MCP server in the organization the signed-in person can use, across all
 * projects. Renders nothing unless the organization serves one, so it never
 * points anyone at a URL that would not answer.
 */
export function OrganizationGatewayButton(): JSX.Element | null {
  // Discoverability only: a failed read hides the button rather than the page.
  const { data } = useOrganizationGateway(undefined, undefined, {
    throwOnError: false,
  });
  const url = data?.available ? data.url : undefined;
  if (!url) return null;

  return (
    <Dialog>
      <Dialog.Trigger asChild>
        <Button variant="secondary" size="sm">
          <Button.LeftIcon>
            <Network />
          </Button.LeftIcon>
          <Button.Text>Connect all servers</Button.Text>
        </Button>
      </Dialog.Trigger>
      <Dialog.Content className="max-w-xl">
        <Dialog.Header>
          <Dialog.Title>Connect all your servers at once</Dialog.Title>
          <Dialog.Description>
            Add this URL to your AI client and sign in with your organization
            account. It gives you every MCP server in the organization you can
            use, across all projects, and stays up to date as your access
            changes.
          </Dialog.Description>
        </Dialog.Header>
        <div className="border-border flex items-center gap-3 border px-4 py-3">
          <Text className="min-w-0 flex-1 truncate font-mono text-sm">
            {url}
          </Text>
          <CopyButton text={url} size="xs" tooltip="Copy URL" />
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
