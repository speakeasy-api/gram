import type { Meta, StoryObj } from "@storybook/react-vite";

import { Grid } from ".";

const meta: Meta<typeof Grid> = {
  title: "Design System/Grid",
  component: Grid,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Grid>;

const Cell = ({ children }: { children: React.ReactNode }) => (
  <div className="bg-card border p-4 text-sm">{children}</div>
);

export const Columns: Story = {
  render: () => (
    <Grid columns={3} gap={4}>
      {[1, 2, 3, 4, 5, 6].map((n) => (
        <Grid.Item key={n}>
          <Cell>Item {n}</Cell>
        </Grid.Item>
      ))}
    </Grid>
  ),
};

export const Responsive: Story = {
  render: () => (
    <Grid columns={{ xs: 1, md: 2, lg: 4 }} gap={4}>
      {[1, 2, 3, 4].map((n) => (
        <Grid.Item key={n}>
          <Cell>Item {n}</Cell>
        </Grid.Item>
      ))}
    </Grid>
  ),
};

export const Spans: Story = {
  render: () => (
    <Grid columns={4} gap={4}>
      <Grid.Item colSpan={3}>
        <Cell>Spans three columns</Cell>
      </Grid.Item>
      <Grid.Item>
        <Cell>One</Cell>
      </Grid.Item>
    </Grid>
  ),
};
