import type { Meta, StoryObj } from "@storybook/react-vite";

import { HoverCard, HoverCardContent, HoverCardTrigger } from ".";

const meta: Meta<typeof HoverCard> = {
  title: "Design System/HoverCard",
  component: HoverCard,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof HoverCard>;

export const Default: Story = {
  render: () => (
    <HoverCard>
      <HoverCardTrigger className="underline underline-offset-4">
        petstore.listPets
      </HoverCardTrigger>
      <HoverCardContent>
        Returns every pet visible to the caller, paginated.
      </HoverCardContent>
    </HoverCard>
  ),
};
