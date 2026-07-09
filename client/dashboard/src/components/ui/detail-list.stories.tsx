import type { Meta, StoryObj } from "@storybook/react-vite";

import { DetailList } from "@/components/ui/detail-list";

const meta: Meta<typeof DetailList> = {
  title: "UI/DetailList",
  component: DetailList,
  tags: ["autodocs"],
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof DetailList>;

export const Stacked: Story = {
  render: () => (
    <div className="max-w-xl">
      <DetailList orientation="stacked">
        <DetailList.Item label="Status" value="Active" />
        <DetailList.Item label="Region" value="us-east-1" />
        <DetailList.Item label="Created" value="Jan 12, 2026" />
        <DetailList.Item label="Owner" value="ada@speakeasy.com" />
      </DetailList>
    </div>
  ),
};

export const Inline: Story = {
  render: () => (
    <div className="max-w-sm">
      <DetailList orientation="inline">
        <DetailList.Item label="Status" value="Active" />
        <DetailList.Item label="Region" value="us-east-1" />
        <DetailList.Item label="Created" value="Jan 12, 2026" />
        <DetailList.Item label="Owner" value="ada@speakeasy.com" />
      </DetailList>
    </div>
  ),
};

export const InlineLongLabels: Story = {
  render: () => (
    <div className="max-w-sm">
      <DetailList orientation="inline">
        <DetailList.Item label="Deployment ID" value="dep_9f2a1c" />
        <DetailList.Item label="Last successful run" value="2 hours ago" />
        <DetailList.Item label="Toolset" value="Support" />
      </DetailList>
    </div>
  ),
};

export const StackedThreeColumn: Story = {
  render: () => (
    <div className="max-w-2xl">
      <DetailList orientation="stacked" className="grid-cols-3">
        <DetailList.Item label="Requests" value="12,402" />
        <DetailList.Item label="Errors" value="18" />
        <DetailList.Item label="Latency" value="212ms" />
        <DetailList.Item label="Tools" value="6" />
        <DetailList.Item label="Servers" value="2" />
        <DetailList.Item label="Uptime" value="99.98%" />
      </DetailList>
    </div>
  ),
};
