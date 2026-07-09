import type { Meta, StoryObj } from "@storybook/react-vite";
import { Badge } from "../Badge";
import { Card } from ".";

const meta: Meta<typeof Card> = {
  title: "Moonshine/Card",
  component: Card,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Card>;

export const Default: Story = {
  args: {
    children: [
      <Card.Header key="header">This is a card title</Card.Header>,
      <Card.Content key="content">
        Lorem ipsum dolor sit amet consectetur adipiscing elit, parturient
        tellus quam odio mollis. Vel placerat massa fermentum commodo tellus
        congue primis euismod porta, senectus mi sagittis morbi posuere aliquet
        turpis habitant neque, non tempor ac cubilia malesuada condimentum diam
        eget. Elementum iaculis himenaeos varius orci nulla fames dis conubia
        dui, lobortis auctor mi dignissim lectus est vulputate ornare platea,
        pellentesque purus curabitur penatibus placerat suspendisse vehicula
        rutrum.
      </Card.Content>,
      <Card.Footer key="footer" content={{ text: "Footer" }} />,
    ],
  },
};

export const WithNoHeader: Story = {
  args: {
    ...Default.args,
    children: [
      <Card.Content key="content">
        Lorem ipsum dolor sit amet consectetur adipiscing elit, parturient
        tellus quam odio mollis. Vel placerat massa fermentum commodo tellus
        congue primis euismod porta, senectus mi sagittis morbi posuere aliquet
        turpis habitant neque, non tempor ac cubilia malesuada condimentum diam
        eget. Elementum iaculis himenaeos varius orci nulla fames dis conubia
        dui, lobortis auctor mi dignissim lectus est vulputate ornare platea,
        pellentesque purus curabitur penatibus placerat suspendisse vehicula
        rutrum.
      </Card.Content>,
      <Card.Footer key="footer" content={{ text: "Footer" }} />,
    ],
  },
};

export const WithNoFooter: Story = {
  args: {
    ...WithNoHeader.args,
    children: [
      <Card.Header key="header">This is a card title</Card.Header>,
      <Card.Content key="content">
        Lorem ipsum dolor sit amet consectetur adipiscing elit, parturient
        tellus quam odio mollis. Vel placerat massa fermentum commodo tellus
        congue primis euismod porta, senectus mi sagittis morbi posuere aliquet
        turpis habitant neque, non tempor ac cubilia malesuada condimentum diam
        eget. Elementum iaculis himenaeos varius orci nulla fames dis conubia
        dui, lobortis auctor mi dignissim lectus est vulputate ornare platea,
        pellentesque purus curabitur penatibus placerat suspendisse vehicula
        rutrum.
      </Card.Content>,
    ],
  },
};

export const WithInvalidChildren: Story = {
  args: {
    ...Default.args,
    children: [
      <Card.Header key="header">
        Card will not accept invalid children
      </Card.Header>,
      <Card.Content key="content">
        The Card component has a guardrail which prevents any React children
        from being rendered which are not one of:
        <ul className="mt-2 list-disc pl-4">
          <li>
            <code className="font-mono text-sm text-blue-500">Card.Header</code>
          </li>
          <li>
            <code className="font-mono text-sm text-blue-500">
              Card.Content
            </code>
          </li>
          <li>
            <code className="font-mono text-sm text-blue-500">Card.Footer</code>
          </li>
        </ul>
      </Card.Content>,
      <div key="invalid">Won't be rendered</div>,
    ],
  },
};

export const WithHref: Story = {
  args: {
    ...Default.args,
    children: [
      <Card.Header key="header" subheader="Click me now please, really!">
        Interact with me!
      </Card.Header>,
      <Card.Content key="content">
        <Badge>Something went wrong</Badge>
      </Card.Content>,
    ],
    href: "/test",
  },
};

export const WithSubheader: Story = {
  args: {
    children: [
      <Card.Header
        key="header"
        subheader="This is a helpful subheader that provides more context"
      >
        Card with Subheader
      </Card.Header>,
      <Card.Content key="content">
        The subheader helps provide additional context or metadata about the
        card's content. It appears right below the main header with reduced
        emphasis.
      </Card.Content>,
    ],
  },
};

export const WithLongSubheader: Story = {
  args: {
    children: [
      <Card.Header
        key="header"
        subheader="This is a longer subheader that demonstrates how the text wraps when it extends beyond a single line. It should maintain proper spacing and alignment."
      >
        Long Subheader Example
      </Card.Header>,
      <Card.Content key="content">Main content area of the card.</Card.Content>,
    ],
  },
};

export const WithSubheaderAndFooter: Story = {
  args: {
    children: [
      <Card.Header key="header" subheader="Last updated 2 hours ago">
        Complete Example
      </Card.Header>,
      <Card.Content key="content">
        This example shows how the subheader works together with other card
        elements including the footer.
      </Card.Content>,
      <Card.Footer key="footer" content={{ text: "Created by Team" }} />,
    ],
  },
};

export const WithLeftIcon: Story = {
  args: {
    children: [
      <Card.Header
        key="header"
        subheader="With an icon on the left"
        icon={{
          name: "component",
          size: "medium",
        }}
      >
        Card with Left Icon
      </Card.Header>,
      <Card.Content key="content">
        This card demonstrates the use of a left icon.
      </Card.Content>,
    ],
  },
};

export const WithCustomRightElement: Story = {
  args: {
    children: [
      <Card.Header
        key="header"
        subheader="Custom right element"
        rightElement={{
          type: "button",
          label: "Action",
          onClick: () => console.log("clicked"),
        }}
      >
        Card with Button
      </Card.Header>,
      <Card.Content key="content">
        This card has a custom button instead of a chevron.
      </Card.Content>,
    ],
  },
};

export const WithGauge: Story = {
  args: {
    children: [
      <Card.Header
        key="header"
        subheader="With gauge"
        rightElement={{
          type: "gauge",
          value: 75,
        }}
      >
        Card with Gauge
      </Card.Header>,
      <Card.Content key="content">
        This card shows how to combine a custom right element with a gauge.
      </Card.Content>,
    ],
  },
};

export const AsLink: Story = {
  args: {
    children: [
      <Card.Header key="header" subheader="Click me to navigate">
        Card as Link
      </Card.Header>,
      <Card.Content key="content">
        This card acts as a link to another page
      </Card.Content>,
    ],
    href: "/some-page",
  },
};

export const WithGaugeAndLink: Story = {
  args: {
    children: [
      <Card.Header
        key="header"
        subheader="With gauge and navigation"
        rightElement={{
          type: "gauge",
          value: 75,
        }}
      >
        Card with Gauge
      </Card.Header>,
      <Card.Content key="content">
        Since a gauge isn't interactive, the card-level href works fine here.
        Try clicking anywhere on the card.
      </Card.Content>,
    ],
    href: "/dashboard",
  },
};

export const WithActionButtonAndFooterLink: Story = {
  args: {
    children: [
      <Card.Header
        key="header"
        rightElement={{
          type: "button",
          label: "View Details",
          onClick: () => console.log("clicked"),
        }}
      >
        Card with Action Button
      </Card.Header>,
      <Card.Content key="content">Content here</Card.Content>,
      <Card.Footer
        key="footer"
        content={{
          text: "Last updated:",
          link: {
            label: "View history",
            href: "/history",
          },
        }}
      />,
    ],
  },
};

export const ConflictingInteractions: Story = {
  args: {
    children: [
      <Card.Header
        key="header"
        subheader="Button will work, card click will be disabled"
        rightElement={{
          type: "button",
          label: "Action",
          onClick: () => console.log("button clicked"),
        }}
      >
        Card with Button
      </Card.Header>,
      <Card.Content key="content">
        When a Card contains a button element, any card-level interaction
        (href/onClick) will be disabled to prevent confusing UX with nested
        clickable elements. Try clicking the button vs clicking the card.
      </Card.Content>,
    ],
    href: "/will-be-ignored", // This will be ignored due to button presence
  },
};
