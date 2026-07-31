import type { Meta, StoryObj } from "@storybook/react-vite";

import { MiniCard } from ".";

const meta: Meta<typeof MiniCard> = {
  title: "Design System/CardMini",
  component: MiniCard,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof MiniCard>;

export const Default: Story = {
  render: () => (
    <div className="w-96">
      <MiniCard>
        <MiniCard.Title>Petstore</MiniCard.Title>
        <MiniCard.Description>12 tools · 3 environments</MiniCard.Description>
      </MiniCard>
    </div>
  ),
};

export const WithActions: Story = {
  render: () => (
    <div className="w-96">
      <MiniCard>
        <MiniCard.Title>Petstore</MiniCard.Title>
        <MiniCard.Description>12 tools · 3 environments</MiniCard.Description>
        <MiniCard.Actions
          actions={[
            { label: "Rename", icon: "pencil", onClick: () => {} },
            {
              label: "Delete",
              icon: "trash",
              onClick: () => {},
              destructive: true,
            },
          ]}
        />
      </MiniCard>
    </div>
  ),
};
