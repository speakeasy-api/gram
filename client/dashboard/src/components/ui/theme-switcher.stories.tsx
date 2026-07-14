import type { Meta, StoryObj } from "@storybook/react-vite";
import { ThemeSwitcher } from "./theme-switcher";

const meta: Meta<typeof ThemeSwitcher> = {
  title: "UI/ThemeSwitcher",
  component: ThemeSwitcher,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof ThemeSwitcher>;

export const LightMode: Story = {
  globals: { theme: "light" },
};

export const DarkMode: Story = {
  globals: { theme: "dark" },
};
