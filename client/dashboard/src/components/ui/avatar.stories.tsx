import type { Meta, StoryObj } from "@storybook/react-vite";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";

const meta: Meta<typeof Avatar> = {
  title: "UI/Avatar",
  component: Avatar,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Avatar>;

export const WithImage: Story = {
  render: () => (
    <Avatar>
      <AvatarImage
        src="https://avatars.githubusercontent.com/u/9919?v=4"
        alt="GitHub"
      />
      <AvatarFallback>GH</AvatarFallback>
    </Avatar>
  ),
};

export const FallbackOnly: Story = {
  render: () => (
    <Avatar>
      <AvatarImage src="https://broken-image-url.invalid/avatar.png" alt="" />
      <AvatarFallback>AB</AvatarFallback>
    </Avatar>
  ),
};

export const CustomSize: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      <Avatar className="size-6">
        <AvatarFallback className="text-xs">SM</AvatarFallback>
      </Avatar>
      <Avatar>
        <AvatarFallback>MD</AvatarFallback>
      </Avatar>
      <Avatar className="size-12">
        <AvatarFallback className="text-lg">LG</AvatarFallback>
      </Avatar>
    </div>
  ),
};

export const Group: Story = {
  render: () => (
    <div className="flex -space-x-3">
      <Avatar className="ring-background ring-2">
        <AvatarFallback>AD</AvatarFallback>
      </Avatar>
      <Avatar className="ring-background ring-2">
        <AvatarFallback>BC</AvatarFallback>
      </Avatar>
      <Avatar className="ring-background ring-2">
        <AvatarFallback>+3</AvatarFallback>
      </Avatar>
    </div>
  ),
};
