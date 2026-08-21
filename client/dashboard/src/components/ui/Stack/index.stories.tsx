import type { Meta, StoryObj } from "@storybook/react-vite";

import { Stack } from ".";

const meta: Meta<typeof Stack> = {
  title: "Design System/Stack",
  component: Stack,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Stack>;

const Box = ({ children }: { children: React.ReactNode }) => (
  <div className="bg-card border px-3 py-2 text-sm">{children}</div>
);

export const Vertical: Story = {
  render: () => (
    <Stack gap={3}>
      <Box>One</Box>
      <Box>Two</Box>
      <Box>Three</Box>
    </Stack>
  ),
};

export const Horizontal: Story = {
  render: () => (
    <Stack direction="horizontal" gap={3} align="center">
      <Box>One</Box>
      <Box>Two</Box>
      <Box>Three</Box>
    </Stack>
  ),
};

export const SpaceBetween: Story = {
  render: () => (
    <Stack direction="horizontal" justify="space-between" className="w-96">
      <Box>Left</Box>
      <Box>Right</Box>
    </Stack>
  ),
};

export const Responsive: Story = {
  render: () => (
    <Stack
      direction={{ xs: "vertical", md: "horizontal" }}
      gap={{ xs: 2, md: 6 }}
    >
      <Box>One</Box>
      <Box>Two</Box>
    </Stack>
  ),
};
