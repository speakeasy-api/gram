import type { Meta, StoryObj } from "@storybook/react-vite";

import { Dialog } from ".";
import { Button } from "@/components/ui/Button";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { Heading } from "@/components/ui/Heading";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";

const meta: Meta<typeof Dialog> = {
  title: "Design System/Dialog",
  component: Dialog,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Dialog>;

export const Default: Story = {
  render: () => (
    <Dialog defaultOpen>
      <Dialog.Trigger asChild>
        <Button variant="secondary">Regenerate</Button>
      </Dialog.Trigger>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>
            <Heading variant="h4">Regenerate</Heading>
          </Dialog.Title>
          <Dialog.Description>
            Re-runs generation against the current deployment.
          </Dialog.Description>
        </Dialog.Header>
        <Stack gap={2}>
          <Text>Run the following command locally:</Text>
          <CodeSnippet language="bash" code="speakeasy run" copyable />
        </Stack>
        <Dialog.Footer>
          <Dialog.Close asChild>
            <Button variant="tertiary">Cancel</Button>
          </Dialog.Close>
          <Button>Regenerate</Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  ),
};

export const NotCloseable: Story = {
  render: () => (
    <Dialog defaultOpen>
      <Dialog.Content closeable={false}>
        <Dialog.Header>
          <Dialog.Title>
            <Heading variant="h4">Finishing setup</Heading>
          </Dialog.Title>
          <Dialog.Description>This will only take a moment.</Dialog.Description>
        </Dialog.Header>
      </Dialog.Content>
    </Dialog>
  ),
};
