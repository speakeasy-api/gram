import type { Meta, StoryObj } from "@storybook/react-vite";

import { Progress, UsageMeter } from "@/components/ui/progress";

const meta: Meta<typeof Progress> = {
  title: "UI/Progress",
  component: Progress,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Progress>;

export const Default: Story = {
  args: { value: 42 },
};

export const Empty: Story = {
  args: { value: 0 },
};

export const Full: Story = {
  args: { value: 100 },
};

export const CustomMax: Story = {
  args: { value: 340, max: 500 },
};

export const AllTones: Story = {
  render: () => (
    <div className="flex w-64 flex-col gap-3">
      <Progress value={65} tone="neutral" />
      <Progress value={65} tone="success" />
      <Progress value={65} tone="warning" />
      <Progress value={65} tone="destructive" />
      <Progress value={65} tone="information" />
    </div>
  ),
};

export const UsageMeterWithinIncluded: StoryObj<typeof UsageMeter> = {
  render: () => (
    <div className="w-80">
      <UsageMeter
        used={420}
        included={1000}
        labels={{ primary: "420 used", secondary: "1,000 included" }}
      />
    </div>
  ),
};

export const UsageMeterWithOverage: StoryObj<typeof UsageMeter> = {
  render: () => (
    <div className="w-80">
      <UsageMeter
        used={1000}
        included={1000}
        overageUsed={180}
        overageLimit={500}
        labels={{
          primary: "1,180 used",
          secondary: "1,000 included + 500 overage",
        }}
      />
    </div>
  ),
};

export const UsageMeterUnboundedOverage: StoryObj<typeof UsageMeter> = {
  render: () => (
    <div className="w-80">
      <UsageMeter
        used={1000}
        included={1000}
        overageUsed={340}
        labels={{ primary: "1,340 used", secondary: "1,000 included" }}
      />
    </div>
  ),
};

export const UsageMeterNoLabels: StoryObj<typeof UsageMeter> = {
  render: () => (
    <div className="w-80">
      <UsageMeter used={250} included={1000} />
    </div>
  ),
};
