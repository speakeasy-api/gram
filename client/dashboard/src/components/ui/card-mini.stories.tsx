import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";
import { FileCodeIcon, SquareFunctionIcon } from "lucide-react";
import { MiniCard } from "@/components/ui/card-mini";

faker.seed(31);

const meta: Meta<typeof MiniCard> = {
  title: "UI/CardMini",
  component: MiniCard,
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof MiniCard>;

export const Default: Story = {
  render: () => (
    <MiniCard className="w-full max-w-sm">
      <MiniCard.Title>{faker.system.fileName()}</MiniCard.Title>
      <MiniCard.Description>OpenAPI Document</MiniCard.Description>
    </MiniCard>
  ),
};

// MiniCard's slot detection only inspects *direct* children, so the icon +
// name + subtitle cluster must live inside a single <MiniCard.Title> (as the
// real AssetItem usage does) rather than wrapping Title/Description in an
// outer div — an outer div would be treated as an unrecognized "other" child
// and rendered after the actions instead of in the content slot.
export const WithActions: Story = {
  render: () => (
    <MiniCard className="w-full max-w-sm">
      <MiniCard.Title>
        <div className="flex w-full items-center gap-4">
          <FileCodeIcon strokeWidth={1} className="size-12 min-w-12" />
          <div className="flex flex-col">
            <span className="text-base leading-7">petstore.openapi.yaml</span>
            <span className="text-muted-foreground text-xs leading-5">
              OpenAPI Document
            </span>
          </div>
        </div>
      </MiniCard.Title>
      <MiniCard.Actions
        actions={[
          { label: "Download", icon: "download", onClick: () => {} },
          {
            label: "Delete",
            icon: "trash-2",
            onClick: () => {},
            destructive: true,
          },
        ]}
      />
    </MiniCard>
  ),
};

export const FunctionAsset: Story = {
  render: () => (
    <MiniCard className="w-full max-w-sm">
      <MiniCard.Title>
        <div className="flex w-full items-center gap-4">
          <SquareFunctionIcon strokeWidth={1} className="size-12 min-w-12" />
          <div className="flex flex-col">
            <span className="text-base leading-7">send-notification.ts</span>
            <span className="text-muted-foreground font-mono text-xs leading-5">
              nodejs22.x
            </span>
          </div>
        </div>
      </MiniCard.Title>
      <MiniCard.Actions
        actions={[{ label: "Download", icon: "download", onClick: () => {} }]}
      />
    </MiniCard>
  ),
};

export const SmallSize: Story = {
  render: () => (
    <MiniCard size="sm" className="w-full max-w-sm">
      <MiniCard.Title>{faker.system.commonFileName("json")}</MiniCard.Title>
      <MiniCard.Description>{faker.lorem.words(3)}</MiniCard.Description>
    </MiniCard>
  ),
};

export const List: Story = {
  render: () => (
    <div className="flex max-w-sm flex-col gap-3">
      {Array.from({ length: 3 }, () => faker.system.fileName()).map((name) => (
        <MiniCard key={name} size="sm">
          <MiniCard.Title>{name}</MiniCard.Title>
          <MiniCard.Description>{faker.lorem.words(4)}</MiniCard.Description>
          <MiniCard.Actions
            actions={[
              { label: "Download", icon: "download", onClick: () => {} },
            ]}
          />
        </MiniCard>
      ))}
    </div>
  ),
};
