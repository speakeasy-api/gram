import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";

const meta: Meta<typeof HoverCard> = {
  title: "UI/HoverCard",
  component: HoverCard,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof HoverCard>;

export const Default: Story = {
  render: () => (
    <HoverCard>
      <HoverCardTrigger asChild>
        <button className="rounded-md border px-3 py-1.5 text-sm">
          @gram-ai
        </button>
      </HoverCardTrigger>
      <HoverCardContent>
        <div className="flex flex-col gap-1">
          <p className="text-sm font-medium">Gram</p>
          <p className="text-muted-foreground text-sm">
            Turn any API into agent-ready tools.
          </p>
        </div>
      </HoverCardContent>
    </HoverCard>
  ),
};

export const AlignStart: Story = {
  render: () => (
    <HoverCard>
      <HoverCardTrigger asChild>
        <button className="rounded-md border px-3 py-1.5 text-sm">
          Align start
        </button>
      </HoverCardTrigger>
      <HoverCardContent align="start">
        <p className="text-sm">Aligned to the start of the trigger.</p>
      </HoverCardContent>
    </HoverCard>
  ),
};

export const WithIconTrigger: Story = {
  render: () => (
    <HoverCard>
      <HoverCardTrigger asChild>
        <span className="text-muted-foreground cursor-help text-sm underline decoration-dotted">
          production
        </span>
      </HoverCardTrigger>
      <HoverCardContent>
        <div className="flex flex-col gap-1">
          <p className="text-sm font-medium">Production environment</p>
          <p className="text-muted-foreground text-sm">
            Credentials used when tools are invoked from live traffic.
          </p>
        </div>
      </HoverCardContent>
    </HoverCard>
  ),
};

export const OpenDelay: Story = {
  render: () => (
    <HoverCard openDelay={500} closeDelay={200}>
      <HoverCardTrigger asChild>
        <button className="rounded-md border px-3 py-1.5 text-sm">
          Slow to open
        </button>
      </HoverCardTrigger>
      <HoverCardContent>
        <p className="text-sm">
          Opens after a 500ms hover delay and stays open briefly on pointer-out.
        </p>
      </HoverCardContent>
    </HoverCard>
  ),
};
