import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";
import { Sparkline } from "./Sparkline";

faker.seed(53);

function series(base: number, drift: number, points = 30): number[] {
  let value = base;
  return Array.from({ length: points }, () => {
    value += faker.number.int({ min: -5, max: 5 }) + drift;
    return Math.max(0, value);
  });
}

const meta: Meta<typeof Sparkline> = {
  title: "Charts/Sparkline",
  component: Sparkline,
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof Sparkline>;

export const Line: Story = {
  render: () => <Sparkline data={series(50, 0.4)} />,
};

export const Area: Story = {
  render: () => <Sparkline data={series(50, 0.4)} mode="area" />,
};

export const TrendUp: Story = {
  render: () => <Sparkline data={series(20, 1.5)} trendColor />,
};

export const TrendDown: Story = {
  render: () => <Sparkline data={series(80, -1.5)} trendColor />,
};

export const TrendFlat: Story = {
  render: () => <Sparkline data={series(50, 0)} trendColor />,
};

export const CustomSize: Story = {
  render: () => (
    <Sparkline data={series(50, 0.4)} width={200} height={48} strokeWidth={2} />
  ),
};

export const InTableRow: Story = {
  render: () => (
    <table className="border-border w-full max-w-md border-collapse text-sm">
      <tbody>
        {["Tool calls", "Cost", "Latency"].map((label) => (
          <tr key={label} className="border-border border-t">
            <td className="py-2 pr-4">{label}</td>
            <td className="py-2">
              <Sparkline
                data={series(50, faker.number.int({ min: -2, max: 2 }))}
                trendColor
              />
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  ),
};

export const InsufficientData: Story = {
  render: () => <Sparkline data={[0]} />,
};
