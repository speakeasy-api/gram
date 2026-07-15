import type { Meta, StoryObj } from "@storybook/react-vite";
import { Link } from "./link";

const meta: Meta<typeof Link> = {
  title: "UI/Link",
  component: Link,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Link>;

export const Primary: Story = {
  args: {
    href: "/target",
    size: "md",
    children: "Link",
  },
};

export const PrimaryNoUnderline: Story = {
  args: {
    href: "/target",
    size: "md",
    underline: false,
    children: "Link",
  },
};

export const Secondary: Story = {
  args: {
    href: "/target",
    size: "md",
    variant: "secondary",
    children: "Link",
  },
};

export const SecondaryNoUnderline: Story = {
  args: {
    href: "/target",
    size: "md",
    variant: "secondary",
    underline: false,
    children: "Link",
  },
};

export const ExtraSmall: Story = {
  args: {
    href: "/target",
    size: "xs",
    children: "Link",
  },
};

export const Small: Story = {
  args: {
    href: "/target",
    size: "sm",
    children: "Link",
  },
};

export const Medium: Story = {
  args: {
    href: "/target",
    size: "md",
    children: "Link",
  },
};

export const Large: Story = {
  args: {
    href: "/target",
    size: "lg",
    children: "Link",
  },
};

export const WithLeadingAndTrailingIcons: Story = {
  args: {
    href: "/target",
    size: "md",
    iconPrefixName: "external-link",
    iconSuffixName: "external-link",
    children: "Link",
  },
};

export const WithLeadingIcon: Story = {
  args: {
    href: "/target",
    size: "md",
    iconPrefixName: "external-link",
    children: "Link",
  },
};

export const WithTrailingIcon: Story = {
  args: {
    href: "/target",
    size: "md",
    iconSuffixName: "external-link",
    children: "Link",
  },
};

const CustomRouterLink = ({
  to,
  children,
  ...props
}: {
  to: string;
  children: React.ReactNode;
} & React.AnchorHTMLAttributes<HTMLAnchorElement>) => {
  // This simulates a router link component like Next.js Link or React Router Link
  return (
    <a href={to} {...props}>
      {children}
    </a>
  );
};

export const WithAsChild: Story = {
  name: "With asChild",
  args: {
    variant: "primary",
    size: "md",
    underline: true,
    iconPrefixName: undefined,
    iconSuffixName: "external-link",
    children: "Interactive Link",
  },
  argTypes: {
    variant: {
      control: { type: "select" },
      options: ["primary", "secondary"],
      description: "Link variant style",
    },
    size: {
      control: { type: "select" },
      options: ["xs", "sm", "md", "lg"],
      description: "Link size",
    },
    underline: {
      control: { type: "boolean" },
      description: "Show underline on the link",
    },
    iconPrefixName: {
      control: { type: "select" },
      options: [
        undefined,
        "link",
        "external-link",
        "home",
        "user",
        "settings",
        "help-circle",
      ],
      description: "Icon to show before the text",
    },
    iconSuffixName: {
      control: { type: "select" },
      options: [
        undefined,
        "external-link",
        "arrow-right",
        "chevron-right",
        "arrow-up-right",
      ],
      description: "Icon to show after the text",
    },
    children: {
      control: { type: "text" },
      description: "Link text content",
    },
  },
  render: (args) => (
    <div className="flex flex-col gap-4">
      <div>
        <p className="mb-2 text-muted">Basic asChild usage:</p>
        <Link
          asChild
          variant={args.variant}
          underline={args.underline}
          size={args.size}
        >
          <CustomRouterLink to="/custom-route">
            {args.children}
          </CustomRouterLink>
        </Link>
      </div>

      {args.iconPrefixName && (
        <div>
          <p className="mb-2 text-muted">asChild with prefix icon:</p>
          <Link
            asChild
            variant={args.variant}
            size={args.size}
            underline={args.underline}
            iconPrefixName={args.iconPrefixName}
          >
            <CustomRouterLink to="/prefix">{args.children}</CustomRouterLink>
          </Link>
        </div>
      )}

      {args.iconSuffixName && (
        <div>
          <p className="mb-2 text-muted">asChild with suffix icon:</p>
          <Link
            asChild
            variant={args.variant}
            size={args.size}
            underline={args.underline}
            iconSuffixName={args.iconSuffixName}
          >
            <CustomRouterLink to="/suffix">{args.children}</CustomRouterLink>
          </Link>
        </div>
      )}

      {args.iconPrefixName && args.iconSuffixName && (
        <div>
          <p className="mb-2 text-muted">asChild with both icons:</p>
          <Link
            asChild
            variant={args.variant}
            size={args.size}
            underline={args.underline}
            iconPrefixName={args.iconPrefixName}
            iconSuffixName={args.iconSuffixName}
          >
            <CustomRouterLink to="/both">{args.children}</CustomRouterLink>
          </Link>
        </div>
      )}
    </div>
  ),
};
