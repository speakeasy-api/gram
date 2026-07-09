import type { Meta, StoryObj } from "@storybook/react-vite";
import { Dialog } from ".";
import { Button } from "../Button";
import { Heading } from "../Heading";
import { Stack } from "../Stack";
import { Text } from "../Text";
import { CodeSnippet } from "../CodeSnippet";

const meta: Meta<typeof Dialog> = {
  title: "Moonshine/Dialog",
  component: Dialog,
  tags: ["autodocs"],
  parameters: {
    layout: "centered",
  },
};

export default meta;
type Story = StoryObj<typeof Dialog>;

export const Default: Story = {
  args: {
    children: [
      <Dialog.Trigger asChild key="trigger">
        <Button variant="secondary">Open</Button>
      </Dialog.Trigger>,
      <Dialog.Content key="content">
        <Dialog.Header>
          <Dialog.Title>
            <Heading variant="lg">Regenerate</Heading>
          </Dialog.Title>
        </Dialog.Header>
        <Stack direction="vertical" gap={10}>
          <Stack direction="vertical" gap={2}>
            <Heading variant="xs" as="h4">
              GitHub
            </Heading>
            <Text as="p">
              To regenerate this target on github, either use the following CLI
              command or the button below:
            </Text>
            <CodeSnippet
              language="bash"
              code="speakeasy run --github"
              copyable
            />
            <Button>Regenerate On Github</Button>
          </Stack>

          <Stack direction="vertical" gap={2}>
            <Heading variant="xs" as="h4">
              Local
            </Heading>
            <Text as="p">
              To regenerate this target locally, make sure you are in the
              correct directory and then run the following:
            </Text>
            <CodeSnippet language="bash" code="speakeasy run" copyable />
          </Stack>
        </Stack>
        <Dialog.Footer>
          <Dialog.Close asChild>
            <Button>Okay</Button>
          </Dialog.Close>
        </Dialog.Footer>
      </Dialog.Content>,
    ],
  },
};
