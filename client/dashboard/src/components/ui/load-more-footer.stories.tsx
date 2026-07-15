import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";
import { useState } from "react";

import {
  LoadMoreButton,
  LoadMoreFooter,
} from "@/components/ui/load-more-footer";

faker.seed(23);

const meta: Meta<typeof LoadMoreFooter> = {
  title: "UI/LoadMoreFooter",
  component: LoadMoreFooter,
  tags: ["autodocs"],
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof LoadMoreFooter>;

function DemoList({ rows }: { rows: string[] }) {
  return (
    <div className="border-neutral-softest border">
      {rows.map((row) => (
        <div
          key={row}
          className="border-neutral-softest border-b px-4 py-2 last:border-b-0"
        >
          {row}
        </div>
      ))}
    </div>
  );
}

export const HasMore: Story = {
  render: () => {
    const rows = Array.from({ length: 5 }, () => faker.commerce.productName());
    return (
      <div className="max-w-md">
        <DemoList rows={rows} />
        <LoadMoreFooter
          shown={5}
          total={128}
          noun="tools"
          hasMore
          onLoadMore={() => {}}
        />
      </div>
    );
  },
};

export const Loading: Story = {
  render: () => {
    const rows = Array.from({ length: 5 }, () => faker.commerce.productName());
    return (
      <div className="max-w-md">
        <DemoList rows={rows} />
        <LoadMoreFooter
          shown={5}
          total={128}
          noun="tools"
          hasMore
          isLoading
          onLoadMore={() => {}}
        />
      </div>
    );
  },
};

export const EndOfList: Story = {
  render: () => {
    const rows = Array.from({ length: 3 }, () => faker.commerce.productName());
    return (
      <div className="max-w-md">
        <DemoList rows={rows} />
        <LoadMoreFooter
          shown={3}
          total={3}
          noun="tools"
          hasMore={false}
          onLoadMore={() => {}}
        />
      </div>
    );
  },
};

export const CustomEndLabel: Story = {
  render: () => {
    const rows = Array.from({ length: 3 }, () => faker.commerce.productName());
    return (
      <div className="max-w-md">
        <DemoList rows={rows} />
        <LoadMoreFooter
          shown={3}
          total={3}
          noun="findings"
          hasMore={false}
          endLabel="All results loaded"
          onLoadMore={() => {}}
        />
      </div>
    );
  },
};

export const Refreshing: Story = {
  render: () => {
    const rows = Array.from({ length: 3 }, () => faker.commerce.productName());
    return (
      <div className="max-w-md">
        <DemoList rows={rows} />
        <LoadMoreFooter
          shown={3}
          total={3}
          noun="audit logs"
          hasMore={false}
          isRefreshing
          onLoadMore={() => {}}
        />
      </div>
    );
  },
};

export const UnknownTotal: Story = {
  render: () => {
    const rows = Array.from({ length: 4 }, () => faker.commerce.productName());
    return (
      <div className="max-w-md">
        <DemoList rows={rows} />
        <LoadMoreFooter shown={4} noun="chats" hasMore onLoadMore={() => {}} />
      </div>
    );
  },
};

export const Interactive: Story = {
  render: () => {
    function InteractiveDemo() {
      const [shown, setShown] = useState(4);
      const [isLoading, setIsLoading] = useState(false);
      const total = 20;

      const handleLoadMore = () => {
        setIsLoading(true);
        setTimeout(() => {
          setShown((current) => Math.min(total, current + 4));
          setIsLoading(false);
        }, 600);
      };

      return (
        <div className="max-w-md">
          <DemoList
            rows={Array.from({ length: shown }, (_, i) => `Row ${i + 1}`)}
          />
          <LoadMoreFooter
            shown={shown}
            total={total}
            noun="rows"
            hasMore={shown < total}
            isLoading={isLoading}
            onLoadMore={handleLoadMore}
          />
        </div>
      );
    }

    return <InteractiveDemo />;
  },
};

export const BareButton: Story = {
  render: () => <LoadMoreButton hasMore onLoadMore={() => {}} />,
};

export const BareButtonLoading: Story = {
  render: () => <LoadMoreButton hasMore isLoading onLoadMore={() => {}} />,
};

export const BareButtonExhausted: Story = {
  render: () => <LoadMoreButton hasMore={false} onLoadMore={() => {}} />,
};
