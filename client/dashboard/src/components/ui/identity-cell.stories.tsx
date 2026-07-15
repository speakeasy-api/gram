import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";
import { KeyRoundIcon } from "lucide-react";

import { IdentityCell } from "@/components/ui/identity-cell";

faker.seed(11);

const meta: Meta<typeof IdentityCell> = {
  title: "UI/IdentityCell",
  component: IdentityCell,
  tags: ["autodocs"],
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof IdentityCell>;

export const Default: Story = {
  render: () => (
    <IdentityCell name="Ada Lovelace" subtitle="ada@speakeasy.com" />
  ),
};

export const WithImage: Story = {
  render: () => (
    <IdentityCell
      name="Grace Hopper"
      subtitle="grace@speakeasy.com"
      imageUrl="https://avatars.githubusercontent.com/u/9919?v=4"
    />
  ),
};

export const NameOnly: Story = {
  render: () => <IdentityCell name="Alan Turing" />,
};

export const EmailAsName: Story = {
  render: () => <IdentityCell name="svc-deploy-bot@speakeasy.com" />,
};

export const NonUserPrincipal: Story = {
  render: () => (
    <IdentityCell
      name="Production API key"
      subtitle="key_ab12••••cd90"
      fallbackIcon={<KeyRoundIcon className="size-3.5" />}
    />
  ),
};

export const SmallSize: Story = {
  render: () => (
    <IdentityCell name="Ada Lovelace" subtitle="ada@speakeasy.com" size="sm" />
  ),
};

export const List: Story = {
  render: () => (
    <div className="flex max-w-xs flex-col gap-4">
      {Array.from({ length: 4 }, () => ({
        name: faker.person.fullName(),
        email: faker.internet.email(),
      })).map((person) => (
        <IdentityCell
          key={person.email}
          name={person.name}
          subtitle={person.email}
          size="sm"
        />
      ))}
    </div>
  ),
};
