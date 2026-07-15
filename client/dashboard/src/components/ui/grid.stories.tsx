import type { Meta, StoryObj } from "@storybook/react-vite";

import { Grid } from "./grid";
import { createSampleGridChildren } from "@/components/ui/lib/storybookUtils";

const meta: Meta<typeof Grid> = {
  title: "UI/Grid",
  component: Grid,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Grid>;

const columnCount = 6;
export const Default: Story = {
  args: {
    children: createSampleGridChildren(columnCount),
    columns: columnCount,
    gap: 5,
  },
};

export const WithCustomGap: Story = {
  args: {
    ...Default.args,
    gap: 10,
  },
};

export const WithResponsiveColumns: Story = {
  args: {
    ...Default.args,
    columns: { xs: 1, md: 2, lg: 3 },
  },
};

export const WithCustomColspan: Story = {
  args: {
    ...Default.args,
    wrap: false,
    children: createSampleGridChildren(columnCount).map((child, index) => (
      <Grid.Item
        key={child.key}
        colSpan={index === 0 ? 2 : undefined}
        {...child.props}
      >
        {index === 0 ? "Colspan 2" : "Colspan 1"}
      </Grid.Item>
    )),
  },
};

export const WithResponsiveColspan: Story = {
  args: {
    ...WithCustomColspan.args,
    children: createSampleGridChildren(columnCount - 1).map((child, index) => (
      <Grid.Item
        key={child.key}
        colSpan={index === 0 ? { xs: 2, md: 3, lg: 4 } : undefined}
        {...child.props}
      >
        {index === 0 ? "Colspan 2" : "Colspan 1"}
      </Grid.Item>
    )),
  },
};

export const WithInvalidChildren: Story = {
  args: {
    ...Default.args,
    children: [
      ...createSampleGridChildren(columnCount),
      <div key="invalid-child">Invalid child - will be dropped</div>,
    ],
  },
};

export const WithPadding: Story = {
  args: {
    ...Default.args,
    padding: 10,
  },
};

export const WithResponsivePadding: Story = {
  args: {
    ...Default.args,
    padding: { xs: 0, md: 10, lg: 16 },
  },
};

export const WithResponsivePaddingPerSide: Story = {
  args: {
    ...WithResponsivePadding.args,
    padding: {
      xs: 0,
      md: 10,
      lg: { top: 16, right: 0, bottom: 16, left: 0 },
    },
  },
};

export const WithResponsivePaddingPerAxis: Story = {
  args: {
    ...WithResponsivePaddingPerSide.args,
    padding: { xs: 0, md: { x: 10, y: 0 }, lg: { x: 16, y: 0 } },
  },
};

export const WithGridItemPadding: Story = {
  args: {
    ...Default.args,
    children: [
      <Grid.Item key="1" padding={10}>
        Grid Item 1
      </Grid.Item>,
      <Grid.Item
        key="2"
        padding={{
          x: 10,
          y: 5,
        }}
      >
        Grid Item 2
      </Grid.Item>,
    ],
  },
};
