import type { Meta, StoryObj } from "@storybook/react-vite";

import { ResizablePanel } from ".";

const meta: Meta<typeof ResizablePanel> = {
  title: "Design System/ResizablePanel",
  component: ResizablePanel,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof ResizablePanel>;

export const Horizontal: Story = {
  render: () => (
    <div className="h-64 w-[40rem] border">
      <ResizablePanel direction="horizontal">
        <ResizablePanel.Pane defaultSize="35%">
          <div className="h-full p-4 text-sm">Tool list</div>
        </ResizablePanel.Pane>
        <ResizablePanel.Pane>
          <div className="h-full p-4 text-sm">Tool detail</div>
        </ResizablePanel.Pane>
      </ResizablePanel>
    </div>
  ),
};
