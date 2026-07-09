import type { Meta, StoryObj } from "@storybook/react-vite";
import { XYFade } from "@/components/ui/xy-fade";
import { InboxIcon } from "lucide-react";

const meta: Meta<typeof XYFade> = {
  title: "UI/XYFade",
  component: XYFade,
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof XYFade>;

function TallContent() {
  return (
    <div className="flex h-[400px] w-full flex-col items-center justify-center gap-2 bg-[repeating-linear-gradient(45deg,var(--color-muted)_0,var(--color-muted)_10px,transparent_10px,transparent_20px)]">
      <InboxIcon className="text-muted-foreground size-16" />
      <span className="text-muted-foreground text-sm">
        Tall scrollable content
      </span>
    </div>
  );
}

export const Vertical: Story = {
  render: () => (
    <XYFade className="h-[200px] w-full max-w-md" direction="vertical">
      <TallContent />
    </XYFade>
  ),
};

export const Horizontal: Story = {
  render: () => (
    <XYFade className="h-[120px] w-[300px]" direction="horizontal">
      <div className="flex w-[600px] items-center justify-center gap-2 bg-[repeating-linear-gradient(90deg,var(--color-muted)_0,var(--color-muted)_10px,transparent_10px,transparent_20px)] py-8">
        <InboxIcon className="text-muted-foreground size-16" />
        <span className="text-muted-foreground text-sm whitespace-nowrap">
          Wide horizontally-fading content
        </span>
      </div>
    </XYFade>
  ),
};

export const Both: Story = {
  render: () => (
    <XYFade className="h-[200px] w-[300px]" direction="both">
      <TallContent />
    </XYFade>
  ),
};

export const CustomFadeColor: Story = {
  render: () => (
    <div className="bg-primary/10 rounded-lg p-4">
      <XYFade
        className="h-[160px] w-full max-w-md"
        fadeColor="var(--color-primary-foreground)"
        fadeHeight={40}
      >
        <TallContent />
      </XYFade>
    </div>
  ),
};

// Mirrors the EmptyState graphic usage in page-layout.tsx: a fixed-height
// XYFade framing a centered illustration so it fades into the surrounding card.
export const EmptyStateGraphic: Story = {
  render: () => (
    <div className="bg-background flex w-full max-w-sm items-center justify-center rounded-xl border p-8">
      <XYFade className="h-[250px] w-full" fadeColor="var(--background)">
        <InboxIcon className="text-muted-foreground size-24" strokeWidth={1} />
      </XYFade>
    </div>
  ),
};
