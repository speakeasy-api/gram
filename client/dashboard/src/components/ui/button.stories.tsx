import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button, ButtonProps } from "./button";
import { PlusIcon as LucidePlusIcon, ChevronRight } from "lucide-react";
import { fn as storybookActionFn } from "storybook/test";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";

const meta: Meta<typeof Button> = {
  title: "UI/Button",
  component: Button,
  tags: ["autodocs"],
  argTypes: {
    variant: {
      control: { type: "select" },
      options: [
        // New variants
        "brand",
        "primary",
        "secondary",
        "tertiary",
        "destructive-primary",
        "destructive-secondary",
        // Deprecated variants
        "default",
        "destructive",
        "outline",
        "ghost",
        "link",
      ],
    },
    context: {
      control: { type: "select" },
      options: ["product", "marketing"],
      description:
        "Context determines styling - product uses rounded corners, marketing uses full pills",
    },
  },
};

export default meta;

type Story = StoryObj<typeof Button>;

const baseProps: Partial<ButtonProps> = {
  onClick: storybookActionFn<NonNullable<ButtonProps["onClick"]>>(),
};

// === NEW VARIANTS ===

export const Brand: Story = {
  args: {
    ...baseProps,
    children: "Brand Button",
    variant: "brand",
    context: "product",
  },
};

export const BrandMarketing: Story = {
  args: {
    ...baseProps,
    children: "Brand Marketing",
    variant: "brand",
    context: "marketing",
  },
};

export const Primary: Story = {
  args: {
    ...baseProps,
    children: "Primary Button",
    variant: "primary",
  },
};

export const Secondary: Story = {
  args: {
    ...baseProps,
    children: "Secondary Button",
    variant: "secondary",
  },
};

export const Tertiary: Story = {
  args: {
    ...baseProps,
    children: "Tertiary Button",
    variant: "tertiary",
  },
};

export const DestructivePrimary: Story = {
  args: {
    ...baseProps,
    children: "Delete",
    variant: "destructive-primary",
  },
};

export const DestructiveSecondary: Story = {
  args: {
    ...baseProps,
    children: "Delete",
    variant: "destructive-secondary",
  },
};

export const WithLeftIcon: Story = {
  args: {
    ...Primary.args,
    children: (
      <>
        <Button.LeftIcon>
          <PlusIcon />
        </Button.LeftIcon>
        <Button.Text>Add Item</Button.Text>
      </>
    ),
  },
};

export const WithBothIcons: Story = {
  args: {
    ...Primary.args,
    children: (
      <>
        <Button.LeftIcon>
          <PlusIcon />
        </Button.LeftIcon>
        <Button.Text>Continue</Button.Text>
        <Button.RightIcon>
          <ChevronRight />
        </Button.RightIcon>
      </>
    ),
  },
};

export const WithRightIcon: Story = {
  args: {
    ...Primary.args,
    children: (
      <>
        <Button.Text>Next</Button.Text>
        <Button.RightIcon>
          <ChevronRight />
        </Button.RightIcon>
      </>
    ),
  },
};

export const IconAlignment: Story = {
  name: "Icon + Text Alignment (all sizes)",
  render: () => (
    <div className="flex flex-col gap-4">
      {(["xs", "sm", "md", "lg"] as const).map((size) => (
        <div key={size} className="flex items-center gap-3">
          <Button size={size} variant="secondary">
            <Button.LeftIcon>
              <PlusIcon />
            </Button.LeftIcon>
            <Button.Text>Label</Button.Text>
            <Button.RightIcon>
              <ChevronRight />
            </Button.RightIcon>
          </Button>
          <Button size={size} variant="primary">
            <Button.LeftIcon>
              <PlusIcon />
            </Button.LeftIcon>
            <Button.Text>Label</Button.Text>
          </Button>
          <Button size={size} variant="tertiary">
            <Button.Text>Label</Button.Text>
            <Button.RightIcon>
              <ChevronRight />
            </Button.RightIcon>
          </Button>
        </div>
      ))}
    </div>
  ),
};

// Migration guide
export const ValidationExamples: Story = {
  name: "API Validation (Check Console)",
  render: () => (
    <div className="space-y-6">
      <div>
        <h3 className="mb-2 text-sm font-medium">✅ Correct Usage</h3>
        <div className="flex gap-2">
          <Button variant="primary">
            <Button.LeftIcon>
              <PlusIcon />
            </Button.LeftIcon>
            <Button.Text>Explicit Structure</Button.Text>
          </Button>
          <Button variant="primary">Raw text (auto-wrapped)</Button>
        </div>
        <p className="mt-2 text-xs text-muted">
          Raw text is automatically wrapped in Button.Text, similar to native
          HTML buttons.
        </p>
      </div>

      <div>
        <h3 className="mb-2 text-sm font-medium">
          ⚠️ Validation Warnings (Check Console)
        </h3>
        <div className="flex gap-2">
          <Button variant="secondary">
            <Button.LeftIcon>
              <PlusIcon />
            </Button.LeftIcon>
            <span>Custom span element</span>
          </Button>
          <Button variant="secondary">
            <Button.LeftIcon>
              <PlusIcon />
            </Button.LeftIcon>
            <div>No text or aria-label</div>
          </Button>
        </div>
        <p className="mt-2 text-xs text-muted">
          These buttons use invalid child types and will show warnings in the
          console.
        </p>
      </div>
    </div>
  ),
  parameters: {
    docs: {
      description: {
        story:
          "The Button component validates proper usage in development mode, warning about missing accessibility attributes and incorrect child structure.",
      },
    },
  },
};

export const IconMigrationGuide: Story = {
  name: "Icon Usage Guide",
  render: () => (
    <div className="space-y-6">
      <div>
        <h3 className="mb-2 text-sm font-medium">Text + Icon Buttons</h3>
        <div className="flex gap-2">
          <Button variant="primary">
            <Button.LeftIcon>
              <PlusIcon />
            </Button.LeftIcon>
            <Button.Text>Add Item</Button.Text>
          </Button>
          <Button variant="secondary">
            <Button.Text>Continue</Button.Text>
            <Button.RightIcon>
              <ChevronRight />
            </Button.RightIcon>
          </Button>
        </div>
        <p className="mt-2 text-xs text-muted">
          ✅ Use Button with compound components for text + icons
        </p>
      </div>

      <div>
        <h3 className="mb-2 text-sm font-medium">Icon-Only Buttons</h3>
        <p className="text-xs text-muted">
          ✅ Use separate IconButton component for icon-only buttons (see
          IconButton stories)
        </p>
      </div>
    </div>
  ),
  parameters: {
    docs: {
      description: {
        story:
          "**Two Components**: Use Button for text/mixed content, IconButton for icon-only buttons.",
      },
    },
  },
};

export const Small: Story = {
  args: {
    ...Primary.args,
    children: "Small Button",
    size: "sm",
  },
};

export const Large: Story = {
  args: {
    ...Primary.args,
    children: "Large Button",
    size: "lg",
  },
};

// Wrapper for Lucide PlusIcon that allows us to bypass issue
// where the code snippet would render React.forwardRef instead of <PlusIcon />
function PlusIcon(props: React.ComponentProps<typeof LucidePlusIcon>) {
  return <LucidePlusIcon {...props} />;
}

export const AsChild: Story = {
  args: {
    ...Primary.args,
    asChild: true,
    children: <a href="#">Link as Button</a>,
  },
};

// === SIZE VARIANTS ===

export const AllSizes: Story = {
  render: () => (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h3 className="text-sm font-medium">Primary Button Sizes</h3>
        <div className="flex items-center gap-4">
          <Button variant="primary" size="xs">
            Extra Small
          </Button>
          <Button variant="primary" size="sm">
            Small
          </Button>
          <Button variant="primary" size="md">
            Medium
          </Button>
          <Button variant="primary" size="lg">
            Large
          </Button>
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <h3 className="text-sm font-medium">Secondary Button Sizes</h3>
        <div className="flex items-center gap-4">
          <Button variant="secondary" size="xs">
            Extra Small
          </Button>
          <Button variant="secondary" size="sm">
            Small
          </Button>
          <Button variant="secondary" size="md">
            Medium
          </Button>
          <Button variant="secondary" size="lg">
            Large
          </Button>
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <h3 className="text-sm font-medium">Brand Button Sizes</h3>
        <div className="flex items-center gap-4">
          <Button variant="brand" size="xs">
            Extra Small
          </Button>
          <Button variant="brand" size="sm">
            Small
          </Button>
          <Button variant="brand" size="md">
            Medium
          </Button>
          <Button variant="brand" size="lg">
            Large
          </Button>
        </div>
      </div>
    </div>
  ),
};

// === BUTTON COMBINATIONS ===

export const AllVariants: Story = {
  render: () => (
    <div className="flex flex-wrap gap-4">
      <div className="flex flex-col gap-2">
        <h3 className="text-sm font-medium">New Variants - Product Context</h3>
        <div className="flex gap-2">
          <Button variant="brand" context="product">
            Brand
          </Button>
          <Button variant="primary">Primary</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="tertiary">Tertiary</Button>
        </div>
        <div className="flex gap-2">
          <Button variant="destructive-primary">Delete</Button>
          <Button variant="destructive-secondary">Delete</Button>
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <h3 className="text-sm font-medium">
          New Variants - Marketing Context
        </h3>
        <div className="flex gap-2">
          <Button variant="brand" context="marketing">
            Brand Marketing
          </Button>
          <Button variant="primary" context="marketing">
            Primary
          </Button>
          <Button variant="secondary" context="marketing">
            Secondary
          </Button>
          <Button variant="tertiary" context="marketing">
            Tertiary
          </Button>
        </div>
      </div>
    </div>
  ),
};

export const WithStates: Story = {
  render: () => (
    <div className="flex flex-col gap-4">
      <div className="flex gap-2">
        <Button variant="brand">Normal</Button>
        <Button variant="brand" disabled>
          Disabled
        </Button>
      </div>
      <div className="flex gap-2">
        <Button variant="primary">Normal</Button>
        <Button variant="primary" disabled>
          Disabled
        </Button>
      </div>
      <div className="flex gap-2">
        <Button variant="secondary">Normal</Button>
        <Button variant="secondary" disabled>
          Disabled
        </Button>
      </div>
      <div className="flex gap-2">
        <Button variant="destructive-primary">Normal</Button>
        <Button variant="destructive-primary" disabled>
          Disabled
        </Button>
      </div>
    </div>
  ),
};

export const RainbowButtonZIndexFix: Story = {
  render: () => (
    <div className="relative z-50 space-y-4">
      <Button variant="brand">Normal</Button>
    </div>
  ),
};

export const BrandButtonInStackingContexts: Story = {
  name: "Brand Button in Various Stacking Contexts",
  render: () => (
    <div className="space-y-8 p-4">
      <div>
        <h3 className="mb-4 text-sm font-medium">
          ✅ Brand Button Working in All Contexts
        </h3>
        <p className="mb-6 text-sm text-muted">
          The brand button now works correctly in all stacking contexts thanks
          to built-in fixes.
        </p>
      </div>

      <div className="space-y-6">
        <div>
          <h4 className="mb-3 text-sm font-medium">Flex Containers</h4>
          <div className="space-y-3">
            <div className="flex items-center gap-3 rounded border bg-surface-secondary p-3">
              <span>Navigation:</span>
              <Button variant="brand" size="sm">
                <Button.LeftIcon>
                  <PlusIcon />
                </Button.LeftIcon>
                <Button.Text>Add</Button.Text>
              </Button>
            </div>

            <div className="flex min-h-8 max-w-full items-center gap-1.5 truncate rounded border bg-surface-secondary p-2">
              <a className="cursor-pointer rounded px-1.5 text-muted-foreground hover:text-foreground">
                Home
              </a>
              <span className="text-muted-foreground">/</span>
              <a className="cursor-pointer rounded px-1.5 text-muted-foreground hover:text-foreground">
                Projects
              </a>
              <div className="ml-auto">
                <Button variant="brand" size="sm">
                  <Button.LeftIcon>
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <polyline points="4,17 10,11 4,5"></polyline>
                      <line x1="12" x2="20" y1="19" y2="19"></line>
                    </svg>
                  </Button.LeftIcon>
                  <Button.Text>Generate</Button.Text>
                </Button>
              </div>
            </div>
          </div>
        </div>

        <div>
          <h4 className="mb-3 text-sm font-medium">Grid Layouts</h4>
          <div className="grid grid-cols-[1fr_auto] items-center gap-4 rounded border bg-surface-secondary p-3">
            <div>
              <h5 className="font-medium">Project Settings</h5>
              <p className="text-sm text-muted">Configure your project</p>
            </div>
            <Button variant="brand">
              <Button.Text>Save Changes</Button.Text>
            </Button>
          </div>
        </div>

        <div>
          <h4 className="mb-3 text-sm font-medium">High Z-Index Contexts</h4>
          <div className="relative z-[9999] rounded border bg-surface-secondary p-3">
            <div className="flex items-center justify-between">
              <span>Modal Header</span>
              <Button variant="brand" size="sm">
                <Button.Text>Action</Button.Text>
              </Button>
            </div>
          </div>
        </div>

        <div>
          <h4 className="mb-3 text-sm font-medium">Transform Contexts</h4>
          <div className="scale-100 transform rounded border bg-surface-secondary p-3">
            <div className="flex items-center gap-3">
              <span>Transformed container:</span>
              <Button variant="brand" size="sm">
                <Button.Text>Works!</Button.Text>
              </Button>
            </div>
          </div>
        </div>

        <div>
          <h4 className="mb-3 text-sm font-medium">
            Overflow Hidden Containers
          </h4>
          <div className="overflow-hidden rounded border bg-surface-secondary p-3">
            <div className="flex items-center gap-3">
              <span>Clipped container:</span>
              <Button variant="brand" size="sm">
                <Button.Text>Still works!</Button.Text>
              </Button>
            </div>
          </div>
        </div>

        <div>
          <h4 className="mb-3 text-sm font-medium">Complex Nested Layouts</h4>
          <div className="rounded-lg bg-surface-secondary p-4">
            <div className="rounded border bg-surface-primary">
              <div className="flex items-center justify-between border-b p-4">
                <div className="flex items-center gap-2">
                  <span className="font-medium">Complex Layout</span>
                </div>
                <Button variant="brand" size="sm">
                  <Button.LeftIcon>
                    <PlusIcon />
                  </Button.LeftIcon>
                  <Button.Text>Create</Button.Text>
                </Button>
              </div>
              <div className="p-4 pb-6">
                <div className="flex min-h-8 max-w-full items-center gap-1.5">
                  <a className="cursor-pointer rounded px-1.5 text-muted-foreground hover:text-foreground">
                    Workspace
                  </a>
                  <span className="text-muted-foreground">/</span>
                  <a className="cursor-pointer rounded px-1.5 text-muted-foreground hover:text-foreground">
                    Project
                  </a>
                  <span className="text-muted-foreground">/</span>
                  <span className="text-foreground">Current</span>
                  <div className="ml-auto">
                    <Button variant="brand" size="sm">
                      <Button.LeftIcon>
                        <svg
                          xmlns="http://www.w3.org/2000/svg"
                          width="16"
                          height="16"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        >
                          <polyline points="4,17 10,11 4,5"></polyline>
                          <line x1="12" x2="20" y1="19" y2="19"></line>
                        </svg>
                      </Button.LeftIcon>
                      <Button.Text>Generate</Button.Text>
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  ),
  parameters: {
    docs: {
      description: {
        story: `
This story demonstrates the brand button working correctly in various stacking contexts that previously caused issues with the rainbow outline.

**Fixed Issues:**
- ✅ Flex containers with \`items-center\`
- ✅ Containers with \`overflow: hidden\`
- ✅ High z-index contexts
- ✅ Transform contexts
- ✅ Complex nested layouts
- ✅ Grid layouts

**Technical Solution:**
The component now uses \`transform: translateZ(0)\` to create an isolated stacking context and a separate background span element to ensure proper layering of the rainbow outline pseudo-elements.
        `,
      },
    },
  },
};

export const BrandButtonAsChild: Story = {
  render: () => (
    <div className="space-y-8 p-4">
      <div className="flex flex-col gap-6">
        <div className="flex flex-col gap-2">
          <Heading variant="h3">Brand Button as Child</Heading>
          <Type>
            The following is a hyperlink element using the Button's{" "}
            <pre className="mx-1 inline-block rounded bg-surface-secondary px-1.5">
              asChild
            </pre>
            prop.
          </Type>
        </div>
        <div>
          <Button variant="brand" asChild>
            <a href="#">Link as Button</a>
          </Button>
        </div>
      </div>
    </div>
  ),
};
