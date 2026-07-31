import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { ViewToggle } from ".";
import type { ViewMode } from "@/components/ui/ViewToggle/use-view-mode";

const meta: Meta<typeof ViewToggle> = {
  title: "Design System/ViewToggle",
  component: ViewToggle,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof ViewToggle>;

export const Default: Story = {
  render: function Render() {
    const [value, setValue] = useState<ViewMode>("grid");

    return <ViewToggle value={value} onChange={setValue} />;
  },
};
