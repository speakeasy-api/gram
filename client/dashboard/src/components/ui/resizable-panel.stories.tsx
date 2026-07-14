import { faker } from "@faker-js/faker";
import { ResizablePanel } from "./resizable-panel";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { cn } from "@/lib/utils";

const meta: Meta<typeof ResizablePanel> = {
  title: "UI/ResizablePanel",
  component: ResizablePanel,
  parameters: {
    layout: "fullscreen",
  },
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof ResizablePanel>;

faker.seed(42);

const mapPara = (p: string, index: number) => (
  <p key={index} className="mb-4 text-sm">
    {p}
  </p>
);

const panelClasses = "max-h-screen min-h-screen !overflow-y-scroll p-8";

export const Default: Story = {
  args: {
    direction: "horizontal",
    children: [
      <ResizablePanel.Pane
        minSize={25}
        id="1"
        key="1"
        order={1}
        className={panelClasses}
      >
        <div className="m-auto max-w-screen-2xl">
          {faker.lorem.paragraphs(20).split("\n").map(mapPara)}
        </div>
      </ResizablePanel.Pane>,
      <ResizablePanel.Pane
        minSize={25}
        id="2"
        key="2"
        order={2}
        className={panelClasses}
      >
        <div className="m-auto max-w-screen-2xl">
          {faker.lorem.paragraphs(20).split("\n").map(mapPara)}
        </div>
      </ResizablePanel.Pane>,
    ],
  },
};

export const CustomResizeHandle: Story = {
  args: {
    useDefaultHandle: false,
    direction: "horizontal",
    children: [
      <ResizablePanel.Pane
        minSize={25}
        id="1"
        key="1"
        order={1}
        className={panelClasses}
      >
        <div className="m-auto max-w-screen-2xl">
          {faker.lorem.paragraphs(20).split("\n").map(mapPara)}
        </div>
      </ResizablePanel.Pane>,

      <ResizablePanel.ResizeHandle
        key="handle"
        className="relative w-1 bg-blue-500"
      />,

      <ResizablePanel.Pane
        minSize={25}
        id="2"
        key="2"
        order={2}
        className={panelClasses}
      >
        <div className="m-auto max-w-screen-2xl">
          {faker.lorem.paragraphs(20).split("\n").map(mapPara)}
        </div>
      </ResizablePanel.Pane>,
    ],
  },
};

const ResizerWithState = () => {
  const [isResizing, setIsResizing] = useState(false);
  return (
    <ResizablePanel useDefaultHandle={false} direction="horizontal">
      <ResizablePanel.Pane
        minSize={25}
        id="1"
        key="1"
        order={1}
        className={cn(panelClasses, "bg-zinc-800/50")}
      >
        <div className="m-auto max-w-screen-2xl">
          {faker.lorem.paragraphs(20).split("\n").map(mapPara)}
        </div>
      </ResizablePanel.Pane>

      <ResizablePanel.ResizeHandle
        className={`relative w-px ${isResizing ? "bg-blue-500" : "bg-zinc-400"}`}
        onDragging={(dragging) => setIsResizing(dragging)}
      />

      <ResizablePanel.Pane
        minSize={25}
        id="2"
        key="2"
        order={2}
        className={cn(panelClasses, "bg-slate-800/50")}
      >
        <div className="m-auto max-w-screen-2xl">
          {faker.lorem.paragraphs(20).split("\n").map(mapPara)}
        </div>
      </ResizablePanel.Pane>
    </ResizablePanel>
  );
};

export const WithState: Story = {
  render: () => {
    return <ResizerWithState />;
  },
};
