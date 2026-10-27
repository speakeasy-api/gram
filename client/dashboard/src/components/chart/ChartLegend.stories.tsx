import type { Meta, StoryObj } from "@storybook/react-vite";
import { ChartLegend } from "./ChartLegend";
import { ACCENT_RED, GOOD_GREEN } from "./palette";
const meta: Meta<typeof ChartLegend> = {
  title: "Charts/ChartLegend",
  component: ChartLegend,
  tags: ["autodocs"],
};
export default meta;
type Story = StoryObj<typeof ChartLegend>;
export const Default: Story = {
  args: {
    datasets: [
      { label: "Complete", backgroundColor: GOOD_GREEN },
      { label: "Failed", backgroundColor: ACCENT_RED },
    ],
  },
};
