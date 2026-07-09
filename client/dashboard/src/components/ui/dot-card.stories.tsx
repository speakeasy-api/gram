import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";
import { Database, GitBranch, MessageSquare } from "lucide-react";
import { DotCard } from "@/components/ui/dot-card";
import { Badge } from "@/components/ui/moonshine";
import { Type } from "@/components/ui/type";

faker.seed(7);

const meta: Meta<typeof DotCard> = {
  title: "UI/DotCard",
  component: DotCard,
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof DotCard>;

function ServerCardContent({
  name,
  version,
  description,
}: {
  name: string;
  version: string;
  description: string;
}) {
  return (
    <>
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <Type variant="subheading" as="div" className="truncate">
            {name}
          </Type>
          <Type small muted>
            v{version}
          </Type>
        </div>
      </div>
      <Type small muted className="line-clamp-2">
        {description}
      </Type>
    </>
  );
}

export const Default: Story = {
  render: () => (
    <div className="max-w-md">
      <DotCard icon={<GitBranch className="size-8" />}>
        <ServerCardContent
          name="GitHub"
          version="1.4.2"
          description={faker.lorem.sentence(12)}
        />
      </DotCard>
    </div>
  ),
};

export const WithOverlayBadge: Story = {
  render: () => (
    <div className="max-w-md">
      <DotCard
        icon={<MessageSquare className="size-8" />}
        overlay={
          <div className="absolute top-3.5 left-3.5 z-10">
            <Badge variant="success">
              <Badge.Text>Added</Badge.Text>
            </Badge>
          </div>
        }
      >
        <ServerCardContent
          name="MessageSquare"
          version="2.0.1"
          description={faker.lorem.sentence(12)}
        />
      </DotCard>
    </div>
  ),
};

export const Clickable: Story = {
  render: () => (
    <div className="max-w-md">
      <DotCard
        icon={<Database className="size-8" />}
        onClick={() => alert("Card clicked")}
        className="cursor-pointer"
      >
        <ServerCardContent
          name="PostgreSQL"
          version="16.1"
          description={faker.lorem.sentence(12)}
        />
      </DotCard>
    </div>
  ),
};

export const Grid: Story = {
  render: () => {
    const icons = [GitBranch, MessageSquare, Database];
    const cards = Array.from({ length: 4 }, (_, i) => ({
      name: faker.company.name(),
      version: `${faker.number.int({ min: 1, max: 9 })}.${faker.number.int({ min: 0, max: 9 })}.${faker.number.int({ min: 0, max: 9 })}`,
      description: faker.lorem.sentence(14),
      Icon: icons[i % icons.length]!,
    }));

    return (
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        {cards.map((card) => (
          <DotCard key={card.name} icon={<card.Icon className="size-8" />}>
            <ServerCardContent
              name={card.name}
              version={card.version}
              description={card.description}
            />
          </DotCard>
        ))}
      </div>
    );
  },
};
