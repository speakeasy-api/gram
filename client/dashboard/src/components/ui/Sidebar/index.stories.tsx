import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from ".";

const meta: Meta<typeof Sidebar> = {
  title: "Design System/Sidebar",
  component: Sidebar,
  tags: ["autodocs"],
  parameters: { layout: "fullscreen" },
};

export default meta;
type Story = StoryObj<typeof Sidebar>;

export const Default: Story = {
  render: () => (
    <SidebarProvider>
      <Sidebar>
        <SidebarHeader className="px-4 py-3 font-medium">Gram</SidebarHeader>
        <SidebarContent>
          <SidebarMenu>
            {["Overview", "Servers", "Tools", "Logs"].map((label) => (
              <SidebarMenuItem key={label} className="px-4 py-1.5 text-sm">
                {label}
              </SidebarMenuItem>
            ))}
          </SidebarMenu>
        </SidebarContent>
        <SidebarFooter className="px-4 py-3 text-xs">Signed in</SidebarFooter>
      </Sidebar>
      <SidebarInset className="p-4">
        <SidebarTrigger />
        <p className="pt-4 text-sm">Page content sits in the inset.</p>
      </SidebarInset>
    </SidebarProvider>
  ),
};
