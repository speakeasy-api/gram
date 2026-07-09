import type { Meta, StoryObj } from "@storybook/react-vite";
import { Text } from ".";
import { Stack } from "../Stack";

const meta: Meta<typeof Text> = {
  title: "Moonshine/Text",
  component: Text,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Text>;

export const Default: Story = {
  args: {
    children: "Default text example",
  },
};

export const AllVariants: Story = {
  render: () => (
    <Stack gap={2}>
      <Text variant="lg">Large text</Text>
      <Text variant="md">Medium text (default)</Text>
      <Text variant="sm">Small text</Text>
      <Text variant="xs">Extra small text</Text>
    </Stack>
  ),
};

export const WithDifferentElements: Story = {
  render: () => (
    <Stack gap={2}>
      <Text as="p">As a paragraph</Text>
      <Text as="span">As a span</Text>
      <Text as="div">As a div</Text>
      <Text as="label" htmlFor="input1">
        As a label
      </Text>
    </Stack>
  ),
};

export const MutedText: Story = {
  render: () => (
    <Stack gap={2}>
      <Text muted variant="lg">
        Muted large text
      </Text>
      <Text muted variant="md">
        Muted medium text
      </Text>
      <Text muted variant="sm">
        Muted small text
      </Text>
      <Text muted variant="xs">
        Muted extra small text
      </Text>
    </Stack>
  ),
};

export const CombinedExample: Story = {
  render: () => (
    <Stack gap={4}>
      <div>
        <Text variant="lg">Regular large text</Text>{" "}
        <Text variant="lg" muted>
          Muted large text
        </Text>
      </div>
      <div>
        <Text as="span" variant="sm">
          Small span text
        </Text>
        <Text as="span" variant="sm" muted>
          {" "}
          with muted text
        </Text>
      </div>
    </Stack>
  ),
};

export const WhitespaceHandling: Story = {
  render: () => (
    <Stack gap={4}>
      <div>
        <Text variant="sm">Normal whitespace (default):</Text>

        <div className="max-w-[300px] border border-border">
          <Text>
            This is normal text that will wrap naturally when it reaches the end
            of its container.
          </Text>
        </div>
      </div>

      <div>
        <Text variant="sm">Nowrap:</Text>
        <div className="max-w-[300px] border border-border">
          <Text whiteSpace="nowrap">
            This text will not wrap and will overflow its container
          </Text>
        </div>
      </div>
    </Stack>
  ),
};
