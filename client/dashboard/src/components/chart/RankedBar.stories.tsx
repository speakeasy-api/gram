import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";
import { RankedBar, type RankedBarItem } from "./RankedBar";

faker.seed(71);

function buildItems(withSublabel = false): RankedBarItem[] {
  return Array.from({ length: 6 }, () => ({
    label: faker.company.name(),
    value: faker.number.int({ min: 20, max: 900 }),
    sublabel: withSublabel ? faker.internet.domainName() : undefined,
  })).sort((a, b) => b.value - a.value);
}

const meta: Meta<typeof RankedBar> = {
  title: "Charts/RankedBar",
  component: RankedBar,
  parameters: { layout: "padded" },
};

export default meta;

type Story = StoryObj<typeof RankedBar>;

export const Single: Story = {
  render: () => (
    <div className="max-w-md">
      <RankedBar items={buildItems()} />
    </div>
  ),
};

export const RankGradient: Story = {
  render: () => (
    <div className="max-w-md">
      <RankedBar items={buildItems()} colorMode="rank-gradient" />
    </div>
  ),
};

export const WithSublabels: Story = {
  render: () => (
    <div className="max-w-md">
      <RankedBar items={buildItems(true)} colorMode="rank-gradient" />
    </div>
  ),
};

export const WithLinks: Story = {
  render: () => (
    <div className="max-w-md">
      <RankedBar
        items={buildItems().map((item) => ({ ...item, href: "#" }))}
        colorMode="rank-gradient"
      />
    </div>
  ),
};

export const CustomValueFormat: Story = {
  render: () => (
    <div className="max-w-md">
      <RankedBar
        items={buildItems().map((item) => ({
          ...item,
          value: item.value / 100,
        }))}
        formatValue={(v) => `$${v.toFixed(2)}`}
      />
    </div>
  ),
};

export const Empty: Story = {
  render: () => (
    <div className="max-w-md">
      <RankedBar items={[]} />
    </div>
  ),
};
