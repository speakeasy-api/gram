import type { Meta, StoryObj } from "@storybook/react-vite";

import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";

const meta: Meta<typeof Dialog> = {
  title: "UI/Dialog",
  component: Dialog,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Dialog>;

export const WithTrigger: Story = {
  render: () => (
    <Dialog>
      <Dialog.Trigger asChild>
        <Button variant="secondary">Delete API key</Button>
      </Dialog.Trigger>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Delete API key</Dialog.Title>
          <Dialog.Description>
            This will immediately revoke the key. Any integrations using it will
            stop working.
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer>
          <Dialog.Close asChild>
            <Button variant="secondary">Cancel</Button>
          </Dialog.Close>
          <Dialog.Close asChild>
            <Button variant="destructive-primary">Delete</Button>
          </Dialog.Close>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  ),
};

export const DefaultOpen: Story = {
  render: () => (
    <Dialog defaultOpen>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Invite teammate</Dialog.Title>
          <Dialog.Description>
            They&apos;ll receive an email invite to join this project.
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer>
          <Dialog.Close asChild>
            <Button variant="secondary">Cancel</Button>
          </Dialog.Close>
          <Dialog.Close asChild>
            <Button>Send invite</Button>
          </Dialog.Close>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  ),
};

export const WithoutFooter: Story = {
  render: () => (
    <Dialog defaultOpen>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Deployment logs</Dialog.Title>
          <Dialog.Description>
            Streaming latest build output.
          </Dialog.Description>
        </Dialog.Header>
        <pre className="bg-muted max-h-64 overflow-auto rounded-md p-3 text-xs">
          {
            "[12:00:01] Building...\n[12:00:04] Build succeeded\n[12:00:05] Deploying..."
          }
        </pre>
      </Dialog.Content>
    </Dialog>
  ),
};

export const TitleOnly: Story = {
  render: () => (
    <Dialog defaultOpen>
      <Dialog.Content>
        <Dialog.Title className="sr-only">Quick confirm</Dialog.Title>
        <p className="text-sm">
          A dialog can render just a title (kept for screen readers) and free
          form content below it.
        </p>
        <Dialog.Footer>
          <Dialog.Close asChild>
            <Button>Got it</Button>
          </Dialog.Close>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  ),
};
