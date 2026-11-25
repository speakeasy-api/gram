import type { Meta, StoryObj } from "@storybook/react-vite";
import { MemoryRouter } from "react-router";
import { Breadcrumb } from "./";

const meta: Meta<typeof Breadcrumb> = {
  title: "Design System/Breadcrumb",
  component: Breadcrumb,
  tags: ["autodocs"],
  decorators: [
    (Story) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};

export default meta;

type Story = StoryObj<typeof Breadcrumb>;

export const Default: Story = {
  args: {
    items: [
      { url: "/identities", display: "Identities" },
      {
        url: "/identities/user",
        display: "Dániel Kovács",
        isCurrentPage: true,
      },
    ],
  },
};

export const Pending: Story = {
  args: {
    items: [
      { url: "/identities", display: "Identities" },
      { url: "/identities/user", display: "", pending: true },
    ],
  },
};

export const WithoutLink: Story = {
  args: {
    items: [
      { url: "/acme", display: "Acme", disableLink: true },
      { url: "/acme/mcp", display: "MCP", isCurrentPage: true },
    ],
  },
};
