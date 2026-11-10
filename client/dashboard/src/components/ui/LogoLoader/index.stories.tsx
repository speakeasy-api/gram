import type { Meta, StoryObj } from "@storybook/react-vite";

import { LogoLoader, SplashScreen } from ".";

const meta: Meta<typeof LogoLoader> = {
  title: "Design System/LogoLoader",
  component: LogoLoader,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof LogoLoader>;

export const Default: Story = {};

export const InPage: Story = {
  render: () => (
    <div className="flex h-[400px] border">
      <SplashScreen />
    </div>
  ),
};
