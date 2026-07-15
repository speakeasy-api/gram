import type { Meta, StoryObj } from "@storybook/react-vite";
import { Download } from "lucide-react";
import { useState } from "react";

import { CopyButton } from "@/components/ui/copy-button";
import { Type } from "@/components/ui/type";

const meta: Meta<typeof CopyButton> = {
  title: "UI/CopyButton",
  component: CopyButton,
  tags: ["autodocs"],
  args: {
    text: "gram_sk_live_abc123",
  },
};

export default meta;

type Story = StoryObj<typeof CopyButton>;

export const Default: Story = {};

export const WithTooltip: Story = {
  args: {
    tooltip: "Copy API key",
  },
};

export const Sizes: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      <CopyButton text="copy me" size="icon-sm" />
      <CopyButton text="copy me" size="icon" />
      <CopyButton text="copy me" size="inline" />
    </div>
  ),
};

export const CustomIcon: Story = {
  args: {
    icon: Download,
    tooltip: "Download instead of copy",
  },
};

export const AbsoluteOverCodeBlock: Story = {
  render: () => (
    <div className="relative w-96 rounded-md border p-3">
      <CopyButton text="speakeasy run --github" absolute tooltip="Copy" />
      <pre className="text-xs">speakeasy run --github</pre>
    </div>
  ),
};

function CopyWithCallback() {
  const [count, setCount] = useState(0);

  return (
    <div className="flex items-center gap-3">
      <CopyButton
        text="hello from gram"
        tooltip="Copy text"
        onCopy={() => setCount((c) => c + 1)}
      />
      <Type variant="small" muted>
        Copied {count} time{count === 1 ? "" : "s"}
      </Type>
    </div>
  );
}

export const WithOnCopyCallback: Story = {
  render: () => <CopyWithCallback />,
};
