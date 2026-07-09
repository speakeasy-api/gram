import type { Meta, StoryObj } from "@storybook/react-vite";
import { Stack } from ".";
import { createSampleChildren } from "@/components/ui/moonshine/lib/storybookUtils";
import { cn } from "@/components/ui/moonshine/lib/utils";

const meta: Meta<typeof Stack> = {
  title: "Moonshine/Stack",
  component: Stack,
  tags: ["autodocs"],
  argTypes: {
    direction: {
      control: "radio",
      options: ["horizontal", "vertical"],
      table: {
        defaultValue: { summary: "vertical" },
      },
    },
    gap: {
      control: "number",
      options: [0, 1, 2, 3, 4, 5, 6, 8, 10, 12, 16],
      table: {
        defaultValue: { summary: "0" },
      },
    },
    align: {
      control: "radio",
      options: ["stretch", "start", "center", "end", "baseline"],
      table: {
        defaultValue: { summary: "stretch" },
      },
    },
    justify: {
      control: "radio",
      options: ["start", "center", "end", "space-between", "space-evenly"],
      table: {
        defaultValue: { summary: "start" },
      },
    },
    wrap: {
      control: "radio",
      options: ["wrap", "nowrap"],
      table: {
        defaultValue: { summary: "nowrap" },
      },
    },
  },
};

export default meta;
type Story = StoryObj<typeof Stack>;

export const Default: Story = {
  args: {
    children: createSampleChildren(3),
    gap: 0,
  },
};

export const WithCustomGap: Story = {
  args: {
    ...Default.args,
    gap: 5,
  },
};

export const HorizontalDirection: Story = {
  args: {
    ...Default.args,
    direction: "horizontal",
  },
};

export const ResponsiveDirection: Story = {
  args: {
    ...Default.args,
    direction: { xs: "vertical", md: "horizontal" },
  },
};

export const ResponsiveGap: Story = {
  args: {
    ...Default.args,
    gap: { xs: 2, md: 5, lg: 10, xl: 12 },
  },
};

export const Padding: Story = {
  args: {
    ...Default.args,
    padding: 2,
  },
};

export const ResponsivePadding: Story = {
  args: {
    ...Default.args,
    padding: { xs: 2, md: 4, lg: 6, xl: 8 },
  },
};

export const PaddingPerSide: Story = {
  args: {
    ...Default.args,
    padding: { top: 12, right: 0, bottom: 12, left: 0 },
  },
};

export const ResponsivePaddingPerSide: Story = {
  args: {
    ...Default.args,
    padding: { xs: 0, md: 0, lg: 0, xl: { x: 10, y: 12 } },
  },
};

export const JustifyStart = {
  args: {
    ...Default.args,
    direction: "horizontal",
    justify: "start",
  },
};

export const JustifyCenter = {
  args: {
    ...Default.args,
    direction: "horizontal",
    justify: "center",
  },
};

export const JustifyEnd = {
  args: {
    ...Default.args,
    direction: "horizontal",
    justify: "end",
  },
};

export const JustifyCenterWithGap = {
  args: {
    ...Default.args,
    direction: "horizontal",
    justify: "center",
    gap: 4,
  },
};

export const SpaceEvenly = {
  args: {
    ...Default.args,
    direction: "horizontal",
    justify: "space-evenly",
  },
};

export const AlignStretchHorizontal: Story = {
  decorators: [
    (Story) => (
      <div className="h-[200px] rounded-md border border-border">
        <Story />
      </div>
    ),
  ],
  args: {
    direction: "horizontal",
    gap: 2,
    align: "stretch",
    children: [
      <div key="1" className="h-full bg-card p-4 text-card-foreground">
        Short content
      </div>,
      <div key="2" className="h-full bg-muted p-4 text-body">
        This item has more content so it's taller
        <br />
        Multiple lines
      </div>,
      <div key="3" className="h-full bg-accent p-4 text-accent-foreground">
        Short content
      </div>,
    ],
  },
};

export const AlignStretchVertical: Story = {
  decorators: [
    (Story) => (
      <div className="w-[400px] rounded-md border border-border">
        <Story />
      </div>
    ),
  ],
  args: {
    direction: "vertical",
    gap: 2,
    align: "stretch",
    children: [
      <div key="1" className="w-full rounded-md bg-muted p-4 text-body">
        Narrow content
      </div>,
      <div key="2" className="w-full rounded-md bg-muted p-4 text-body">
        This item has more content so it's wider than others
      </div>,
      <div key="3" className="w-full rounded-md bg-muted p-4 text-body">
        Narrow content
      </div>,
    ],
  },
};

export const AlignStretchWithCenter: Story = {
  decorators: [
    (Story) => (
      <div className="h-[200px] rounded-md border border-border">
        <Story />
      </div>
    ),
  ],
  args: {
    direction: "horizontal",
    gap: 2,
    align: "stretch",
    children: [
      <div
        key="1"
        className="flex items-center justify-center rounded-md bg-muted p-4 text-body"
      >
        Short
      </div>,
      <div
        key="2"
        className="flex items-center justify-center rounded-md bg-muted p-4 text-body"
      >
        This item has
        <br />
        multiple lines
        <br />
        of content
      </div>,
      <div
        key="3"
        className="flex items-center justify-center rounded-md bg-muted p-4 text-body"
      >
        Short
      </div>,
    ],
  },
};

export const ResponsiveJustify = {
  args: {
    ...Default.args,
    direction: "horizontal",
    justify: {
      xs: "start",
      md: "center",
      lg: "space-evenly",
    },
    align: "center",
  },
};

export const ResponsiveAlign: Story = {
  args: {
    ...Default.args,
    direction: "vertical",
    align: { xs: "start", md: "center", lg: "end" },
  },
};

export const NavigationBar: Story = {
  args: {
    direction: "horizontal",
    justify: "space-between",
    padding: { x: 4, y: 2 },
    align: "center",
    children: [
      <div key="logo">Logo</div>,
      <Stack key="nav-items" direction="horizontal" gap={4}>
        {["Home", "Products", "About", "Contact"].map((item) => (
          <div key={item}>{item}</div>
        ))}
      </Stack>,
      <div key="actions">Sign In</div>,
    ],
  },
};

export const FormLayout: Story = {
  args: {
    direction: "vertical",
    gap: 4,
    children: [
      <Stack key="name-field" direction="vertical" gap={1}>
        <div>Name</div>
        <div>Input field</div>
      </Stack>,
      <Stack key="email-field" direction="vertical" gap={1}>
        <div>Email</div>
        <div>Input field</div>
      </Stack>,
      <Stack key="actions" direction="horizontal" justify="end" gap={2}>
        <div>Cancel</div>
        <div>Submit</div>
      </Stack>,
    ],
  },
};

export const CardLayout: Story = {
  args: {
    direction: "vertical",
    gap: 4,
    padding: 4,
    children: [
      <Stack
        key="header"
        direction="horizontal"
        justify="space-between"
        align="center"
      >
        <div>Card Title</div>
        <div>⋮</div>
      </Stack>,
      <div key="content">Main content goes here...</div>,
      <Stack key="footer" direction="horizontal" justify="end" gap={2}>
        <div>Cancel</div>
        <div>Save</div>
      </Stack>,
    ],
  },
};

export const DialogLayout: Story = {
  args: {
    direction: "vertical",
    gap: 4,
    padding: 4,
    children: [
      <Stack
        key="header"
        direction="horizontal"
        justify="space-between"
        align="center"
      >
        <div>Dialog Title</div>
        <div>✕</div>
      </Stack>,
      <Stack key="content" direction="vertical" gap={2}>
        <div>Dialog content goes here...</div>
        <div>More content...</div>
      </Stack>,
      <Stack key="footer" direction="horizontal" justify="end" gap={2}>
        <div>Cancel</div>
        <div>Confirm</div>
      </Stack>,
    ],
  },
};

export const MediaWithContent: Story = {
  args: {
    direction: "horizontal",
    gap: 4,
    align: "start",
    children: [
      <div
        key="media"
        className="flex size-24 items-center justify-center rounded-md bg-muted"
      >
        Image
      </div>,
      <Stack key="content" direction="vertical" gap={2}>
        <div className="font-semibold">Title</div>
        <div className="text-body">Description text goes here...</div>
        <Stack direction="horizontal" gap={2}>
          <div className="rounded-full bg-secondary px-3 py-1 text-sm text-secondary-foreground">
            Tag 1
          </div>
          <div className="rounded-full bg-secondary px-3 py-1 text-sm text-secondary-foreground">
            Tag 2
          </div>
        </Stack>
      </Stack>,
    ],
  },
};

export const NestedResponsiveLayout: Story = {
  args: {
    direction: { xs: "vertical", md: "horizontal" },
    gap: { xs: 2, md: 4 },
    align: "stretch",
    children: [
      <Stack key="sidebar" direction="vertical" gap={2} padding={2}>
        <div>Sidebar Item 1</div>
        <div>Sidebar Item 2</div>
      </Stack>,
      <Stack key="main" direction="vertical" gap={4} padding={4}>
        <div>Main Content</div>
        <Stack direction="horizontal" justify="space-between">
          <div>Item 1</div>
          <div>Item 2</div>
          <div>Item 3</div>
        </Stack>
      </Stack>,
    ],
  },
};

export const WrapItems: Story = {
  args: {
    direction: "horizontal",
    gap: 2,
    wrap: "wrap",
    children: Array.from({ length: 10 }, (_, i) => (
      <div key={i} className="w-[150px] rounded-md bg-muted p-4 text-body">
        Item {i + 1}
      </div>
    )),
  },
};

export const WrapReverse: Story = {
  args: {
    ...WrapItems.args,
    wrap: "wrap",
  },
};

export const ResponsiveWrap: Story = {
  args: {
    direction: "horizontal",
    wrap: { xs: "wrap", md: "nowrap" },
    gap: { xs: 2, md: 4 },
    children: Array.from({ length: 6 }, (_, i) => (
      <div
        key={i}
        className="w-[120px] flex-1 rounded-md bg-muted p-4 text-body"
      >
        Navigation Item {i + 1}
      </div>
    )),
  },
};

export const WrapWithAlignment: Story = {
  args: {
    ...WrapItems.args,
    align: "center",
    children: Array.from({ length: 8 }, (_, i) => (
      <div
        key={i}
        className={cn(
          "flex w-[150px] items-center justify-center rounded-md bg-muted p-4 text-body",
          i % 2 === 0 ? "h-[80px]" : "h-[120px]",
        )}
      >
        Item {i + 1}
      </div>
    )),
  },
};

export const Breadcrumbs: Story = {
  args: {
    direction: "horizontal",
    gap: 2,
    align: "center",
    children: [
      <div key="home">Home</div>,
      <div key="separator1">/</div>,
      <div key="products">Products</div>,
      <div key="separator2">/</div>,
      <div key="category">Electronics</div>,
      <div key="separator3">/</div>,
      <div key="current" className="text-body">
        Laptops
      </div>,
    ],
  },
};

export const Pagination: Story = {
  args: {
    direction: "horizontal",
    gap: 2,
    align: "center",
    children: [
      <button
        key="prev"
        className="rounded-md bg-secondary px-4 py-2 text-secondary-foreground"
      >
        Previous
      </button>,
      <Stack key="pages" direction="horizontal" gap={1}>
        {[1, 2, 3, "...", 8, 9, 10].map((page, i) => (
          <div
            key={i}
            className={cn(
              "rounded-md px-3 py-2",
              page === 1
                ? "bg-primary text-primary-foreground"
                : "bg-secondary text-secondary-foreground",
            )}
          >
            {page}
          </div>
        ))}
      </Stack>,
      <button
        key="next"
        className="rounded-md bg-secondary px-4 py-2 text-secondary-foreground"
      >
        Next
      </button>,
    ],
  },
};

export const FooterLayout: Story = {
  args: {
    direction: { xs: "vertical", md: "horizontal" },
    justify: "space-between",
    padding: { x: 6, y: 8 },
    gap: { xs: 8, md: 4 },
    children: [
      <Stack key="company" direction="vertical" gap={4}>
        <div className="text-2xl font-bold">Company</div>
        <Stack direction="vertical" gap={2}>
          <div className="text-body hover:text-foreground">About Us</div>
          <div className="text-body hover:text-foreground">Careers</div>
          <div className="text-body hover:text-foreground">Press</div>
        </Stack>
      </Stack>,
      <Stack key="resources" direction="vertical" gap={4}>
        <div className="text-2xl font-bold">Resources</div>
        <Stack direction="vertical" gap={2}>
          <div className="text-body hover:text-foreground">Blog</div>
          <div className="text-body hover:text-foreground">Documentation</div>
          <div className="text-body hover:text-foreground">Help Center</div>
        </Stack>
      </Stack>,
      <Stack key="legal" direction="vertical" gap={4}>
        <div className="text-2xl font-bold">Legal</div>
        <Stack direction="vertical" gap={2}>
          <div className="text-body hover:text-foreground">Privacy</div>
          <div className="text-body hover:text-foreground">Terms</div>
          <div className="text-body hover:text-foreground">Security</div>
        </Stack>
      </Stack>,
      <Stack key="newsletter" direction="vertical" gap={4}>
        <div className="text-2xl font-bold">Newsletter</div>
        <Stack direction="vertical" gap={2}>
          <div>Subscribe to our newsletter</div>
          <Stack direction="horizontal" gap={2}>
            <div className="flex-1 rounded-md bg-muted px-3 py-2">
              Email input
            </div>
            <button className="rounded-md bg-primary px-4 py-2 text-primary-foreground">
              Subscribe
            </button>
          </Stack>
        </Stack>
      </Stack>,
    ],
  },
};

export const SidebarNavigation: Story = {
  decorators: [
    (Story) => (
      <div className="h-[400px] w-60 rounded-lg border border-border">
        <Story />
      </div>
    ),
  ],
  args: {
    direction: "vertical",
    padding: 2,
    gap: 1,
    children: [
      <Stack
        key="user"
        direction="horizontal"
        gap={3}
        padding={2}
        align="center"
      >
        <div className="size-8 rounded-full bg-muted" />
        <Stack direction="vertical" gap={0}>
          <div className="font-semibold">John Doe</div>
          <div className="text-sm text-body">Admin</div>
        </Stack>
      </Stack>,
      <div key="divider" className="my-2 border-t border-border" />,
      ...["Dashboard", "Analytics", "Projects", "Tasks", "Messages"].map(
        (item) => (
          <div
            key={item}
            className={cn(
              "rounded-md px-4 py-2",
              item === "Dashboard"
                ? "bg-accent text-accent-foreground"
                : "hover:bg-muted/50",
            )}
          >
            {item}
          </div>
        ),
      ),
      <div key="spacer" className="flex-1" />,
      <Stack key="footer" direction="vertical" gap={2} padding={2}>
        <div className="text-sm text-body">Settings</div>
        <div className="text-sm text-body">Help</div>
        <div className="text-sm text-body">Logout</div>
      </Stack>,
    ],
  },
};

export const ResponsiveStack: Story = {
  args: {
    direction: { xs: "vertical", md: "horizontal" },
    gap: { xs: 2, md: 4 },
    align: { xs: "stretch", md: "center" },
    justify: { xs: "start", md: "space-between" },
    children: [
      <Stack.Item key="1" grow={{ xs: false, md: true }}>
        <div className="rounded-md bg-muted p-4 text-body">First</div>
      </Stack.Item>,
      <Stack.Item key="2" grow={{ xs: false, md: true }}>
        <div className="rounded-md bg-muted p-4 text-body">Second</div>
      </Stack.Item>,
    ],
  },
};

export const BasicUsage: Story = {
  args: {
    children: [
      <div key="1" className="rounded-md bg-muted p-4 text-body">
        First
      </div>,
      <div key="2" className="rounded-md bg-muted p-4 text-body">
        Second
      </div>,
      <div key="3" className="rounded-md bg-muted p-4 text-body">
        Third
      </div>,
    ],
  },
};

export const StackItemBasic: Story = {
  args: {
    direction: "horizontal",
    children: [
      <Stack.Item key="1">
        <div className="rounded-md bg-muted p-4 text-body">Fixed width</div>
      </Stack.Item>,
      <Stack.Item key="2" grow>
        <div className="rounded-md bg-muted p-4 text-body">
          Grows to fill space
        </div>
      </Stack.Item>,
      <Stack.Item key="3">
        <div className="rounded-md bg-muted p-4 text-body">Fixed width</div>
      </Stack.Item>,
    ],
  },
};

export const StackItemGrowCombinations: Story = {
  args: {
    direction: "horizontal",
    gap: 2,
    children: [
      <Stack.Item key="1" grow>
        <div className="rounded-md bg-muted p-4 text-body">Grow</div>
      </Stack.Item>,
      <Stack.Item key="2">
        <div className="rounded-md bg-muted p-4 text-body">Fixed</div>
      </Stack.Item>,
      <Stack.Item key="3" grow>
        <div className="rounded-md bg-muted p-4 text-body">Grow</div>
      </Stack.Item>,
    ],
  },
};

export const ResponsiveStackItem: Story = {
  args: {
    direction: "horizontal",
    gap: 2,
    children: [
      <Stack.Item key="1" grow={{ xs: true, md: false }}>
        <div className="rounded-md bg-muted p-4 text-body">
          Grows on xsall screens
        </div>
      </Stack.Item>,
      <Stack.Item key="2" grow={{ xs: false, md: true }}>
        <div className="rounded-md bg-muted p-4 text-body">
          Grows on medium+ screens
        </div>
      </Stack.Item>,
    ],
  },
};

export const AlignmentDemo: Story = {
  decorators: [
    (Story) => (
      <div className="h-[200px] rounded-md border border-border">
        <Story />
      </div>
    ),
  ],
  args: {
    direction: "horizontal",
    gap: 2,
    align: "center",
    children: [
      <div key="1" className="h-[50px] rounded-md bg-muted p-4 text-body">
        Short
      </div>,
      <div key="2" className="h-[100px] rounded-md bg-muted p-4 text-body">
        Medium
      </div>,
      <div key="3" className="h-[150px] rounded-md bg-muted p-4 text-body">
        Tall
      </div>,
    ],
  },
};

export const JustifyDemo: Story = {
  args: {
    direction: "horizontal",
    gap: 2,
    justify: "space-between",
    children: [
      <Stack key="left" direction="horizontal" gap={2}>
        <div className="rounded-md bg-muted p-4 text-body">Left 1</div>
        <div className="rounded-md bg-muted p-4 text-body">Left 2</div>
      </Stack>,
      <Stack key="right" direction="horizontal" gap={2}>
        <div className="rounded-md bg-muted p-4 text-body">Right 1</div>
        <div className="rounded-md bg-muted p-4 text-body">Right 2</div>
      </Stack>,
    ],
  },
};

export const NestedStacksDemo: Story = {
  args: {
    direction: "vertical",
    gap: 4,
    children: [
      <Stack
        key="header"
        direction="horizontal"
        justify="space-between"
        align="center"
      >
        <Stack.Item grow>
          <div className="rounded-md bg-muted p-4 text-body">
            Header Content
          </div>
        </Stack.Item>
        <Stack direction="horizontal" gap={2}>
          <div className="rounded-md bg-muted p-4 text-body">Action 1</div>
          <div className="rounded-md bg-muted p-4 text-body">Action 2</div>
        </Stack>
      </Stack>,
      <Stack key="body" direction="horizontal" gap={4}>
        <Stack.Item grow>
          <div className="rounded-md bg-muted p-4 text-body">Main Content</div>
        </Stack.Item>
        <Stack direction="vertical" gap={2}>
          <div className="rounded-md bg-muted p-4 text-body">Sidebar 1</div>
          <div className="rounded-md bg-muted p-4 text-body">Sidebar 2</div>
        </Stack>
      </Stack>,
    ],
  },
};

export const CommonResponsivePatterns: Story = {
  args: {
    direction: { xs: "vertical", md: "horizontal" },
    gap: { xs: 2, md: 4 },
    children: [
      <Stack.Item key="main" grow>
        <div className="rounded-md bg-muted p-4 text-body">Main Content</div>
      </Stack.Item>,
      <Stack key="sidebar" direction="vertical" gap={2}>
        <div className="rounded-md bg-muted p-4 text-body">Sidebar Item 1</div>
        <div className="rounded-md bg-muted p-4 text-body">Sidebar Item 2</div>
        <div className="rounded-md bg-muted p-4 text-body">Sidebar Item 3</div>
      </Stack>,
    ],
  },
};
