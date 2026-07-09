import type { Meta, StoryObj } from "@storybook/react-vite";
import { IconButton } from "./";
import {
  PlusIcon as LucidePlusIcon,
  Heart,
  Settings,
  Search,
} from "lucide-react";
import { fn as storybookActionFn } from "storybook/test";

const meta: Meta<typeof IconButton> = {
  title: "Moonshine/IconButton",
  component: IconButton,
  tags: ["autodocs"],
  argTypes: {
    variant: {
      control: { type: "select" },
      options: [
        "brand",
        "primary",
        "secondary",
        "tertiary",
        "destructive-primary",
        "destructive-secondary",
        "default",
        "destructive",
        "outline",
        "ghost",
        "link",
      ],
    },
    size: {
      control: { type: "select" },
      options: ["xs", "sm", "md", "lg"],
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

type Story = StoryObj<typeof IconButton>;

// Wrapper for Lucide PlusIcon
function PlusIcon(props: React.ComponentProps<typeof LucidePlusIcon>) {
  return <LucidePlusIcon {...props} />;
}

const baseProps = {
  onClick: storybookActionFn(),
  "aria-label": "Example action",
};

// === VARIANTS ===

export const Primary: Story = {
  args: {
    ...baseProps,
    icon: <PlusIcon />,
    variant: "primary",
    "aria-label": "Add item",
  },
};

export const Secondary: Story = {
  args: {
    ...baseProps,
    icon: <Settings />,
    variant: "secondary",
    "aria-label": "Settings",
  },
};

export const Brand: Story = {
  args: {
    ...baseProps,
    icon: <Heart />,
    variant: "brand",
    "aria-label": "Like",
  },
};

export const Tertiary: Story = {
  args: {
    ...baseProps,
    icon: <Search />,
    variant: "tertiary",
    "aria-label": "Search",
  },
};

export const DestructivePrimary: Story = {
  args: {
    ...baseProps,
    icon: <PlusIcon />,
    variant: "destructive-primary",
    "aria-label": "Delete",
  },
};

// === SIZES ===

export const Sizes: Story = {
  render: () => (
    <div className="flex items-end gap-4">
      <IconButton
        size="xs"
        icon={<PlusIcon />}
        variant="primary"
        aria-label="Add"
      />
      <IconButton
        size="sm"
        icon={<PlusIcon />}
        variant="primary"
        aria-label="Add"
      />
      <IconButton
        size="md"
        icon={<PlusIcon />}
        variant="primary"
        aria-label="Add"
      />
      <IconButton
        size="lg"
        icon={<PlusIcon />}
        variant="primary"
        aria-label="Add"
      />
      <div className="text-xs text-muted">xs, sm, md, lg</div>
    </div>
  ),
};

// === VARIANTS GRID ===

export const AllVariants: Story = {
  render: () => (
    <div className="space-y-6">
      <div>
        <h3 className="mb-3 text-sm font-medium">Product Context</h3>
        <div className="flex gap-3">
          <IconButton
            icon={<Heart />}
            variant="brand"
            context="product"
            aria-label="Like"
          />
          <IconButton
            icon={<PlusIcon />}
            variant="primary"
            context="product"
            aria-label="Add"
          />
          <IconButton
            icon={<Settings />}
            variant="secondary"
            context="product"
            aria-label="Settings"
          />
          <IconButton
            icon={<Search />}
            variant="tertiary"
            context="product"
            aria-label="Search"
          />
        </div>
      </div>

      <div>
        <h3 className="mb-3 text-sm font-medium">Marketing Context</h3>
        <div className="flex gap-3">
          <IconButton
            icon={<Heart />}
            variant="brand"
            context="marketing"
            aria-label="Like"
          />
          <IconButton
            icon={<PlusIcon />}
            variant="primary"
            context="marketing"
            aria-label="Add"
          />
          <IconButton
            icon={<Settings />}
            variant="secondary"
            context="marketing"
            aria-label="Settings"
          />
          <IconButton
            icon={<Search />}
            variant="tertiary"
            context="marketing"
            aria-label="Search"
          />
        </div>
      </div>

      <div>
        <h3 className="mb-3 text-sm font-medium">Destructive Variants</h3>
        <div className="flex gap-3">
          <IconButton
            icon={<PlusIcon />}
            variant="destructive-primary"
            aria-label="Delete"
          />
          <IconButton
            icon={<PlusIcon />}
            variant="destructive-secondary"
            aria-label="Delete"
          />
        </div>
      </div>
    </div>
  ),
};

// === STATES ===

export const States: Story = {
  render: () => (
    <div className="space-y-4">
      <div className="flex gap-3">
        <IconButton icon={<PlusIcon />} variant="primary" aria-label="Normal" />
        <IconButton
          icon={<PlusIcon />}
          variant="primary"
          disabled
          aria-label="Disabled"
        />
      </div>
      <div className="flex gap-3">
        <IconButton
          icon={<Settings />}
          variant="secondary"
          aria-label="Normal"
        />
        <IconButton
          icon={<Settings />}
          variant="secondary"
          disabled
          aria-label="Disabled"
        />
      </div>
    </div>
  ),
};

// === ACCESSIBILITY ===

export const Accessibility: Story = {
  render: () => (
    <div className="space-y-4">
      <div>
        <h3 className="mb-2 text-sm font-medium">
          ✅ Good - Required aria-label
        </h3>
        <IconButton
          icon={<PlusIcon />}
          variant="primary"
          aria-label="Add new item to your collection"
        />
        <p className="mt-2 text-xs text-muted">
          Descriptive aria-label helps screen readers understand the button's
          purpose
        </p>
      </div>

      <div>
        <h3 className="mb-2 text-sm font-medium">
          🔍 TypeScript enforces accessibility
        </h3>
        <pre className="rounded bg-muted p-2 text-xs">
          {`// ❌ TypeScript error - missing aria-label
<IconButton icon={<PlusIcon />} variant="primary" />

// ✅ TypeScript happy - aria-label provided
<IconButton
  icon={<PlusIcon />}
  variant="primary"
  aria-label="Add item"
/>`}
        </pre>
      </div>
    </div>
  ),
  parameters: {
    docs: {
      description: {
        story:
          "IconButton enforces accessibility by requiring aria-label at the TypeScript level.",
      },
    },
  },
};
