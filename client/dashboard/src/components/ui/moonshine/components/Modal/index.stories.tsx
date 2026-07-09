import type { Meta, StoryObj } from "@storybook/react-vite";
import { Modal } from ".";
import {
  ModalProvider,
  Screen,
} from "@/components/ui/moonshine/context/ModalContext";
import { useModal } from "@/components/ui/moonshine/hooks/useModal";
import { memo, useCallback, useEffect } from "react";
import { faker } from "@faker-js/faker";
import { Button, Icon } from "@/components/ui/moonshine";

faker.seed(123);

const meta: Meta<typeof Modal> = {
  component: Modal,
  tags: ["autodocs"],
  parameters: {
    layout: "fullscreen",
  },
};

export default meta;

type Story = StoryObj<typeof Modal>;

const Underlay = memo(() => {
  return (
    <div className="fixed inset-0 flex h-screen w-screen flex-col gap-1 bg-surface-secondary p-20">
      {faker.lorem
        .paragraphs(30, "\n")
        .split("\n")
        .map((paragraph, index) => (
          <p key={index} className="text-body-md">
            {paragraph}
          </p>
        ))}
    </div>
  );
});

const ModalRenderer = ({
  closable = false,
  multiScreen = false,
  layout = "default",
  onClose,
}: {
  closable?: boolean;
  multiScreen?: boolean;
  layout?: "default" | "custom";
  onClose?: (screen: Screen) => void;
}) => {
  const { openScreen, pushScreen } = useModal();

  const addScreen = useCallback(() => {
    pushScreen({
      id: faker.string.uuid(),
      title: faker.lorem.sentence({ min: 1, max: 3 }),
      component: <div>{faker.lorem.paragraph()}</div>,
    });
  }, [pushScreen]);

  useEffect(() => {
    openScreen({
      id: "1",
      title: "Lorem ipsum dolor sit amet",
      component: (
        <div className="flex flex-col gap-3 text-body-md">
          {faker.lorem
            .paragraphs(3, "\n")
            .split("\n")
            .map((paragraph, index) => (
              <p key={index}>{paragraph}</p>
            ))}

          {multiScreen && (
            <Button
              variant="secondary"
              size="sm"
              className="w-fit justify-self-end"
              onClick={addScreen}
            >
              <Icon name="plus" />
              Next screen
            </Button>
          )}
        </div>
      ),
    });
  }, [openScreen, multiScreen, addScreen]);

  return (
    <div>
      <Underlay />
      <Modal closable={closable} layout={layout} onClose={onClose} />
    </div>
  );
};

export const Default: Story = {
  render: () => {
    return (
      <ModalProvider>
        <ModalRenderer />
      </ModalProvider>
    );
  },
};

export const Closable: Story = {
  render: () => {
    return (
      <ModalProvider>
        <ModalRenderer closable />
      </ModalProvider>
    );
  },
};

export const MultiScreen: Story = {
  render: () => {
    return (
      <ModalProvider>
        <ModalRenderer closable={false} multiScreen />
      </ModalProvider>
    );
  },
};

export const CustomOnCloseAction: Story = {
  render: () => {
    return (
      <ModalProvider>
        <ModalRenderer
          closable
          onClose={(screen) => alert(`closed screen with id ${screen.id}`)}
        />
      </ModalProvider>
    );
  },
};

const CustomModal = () => {
  const { openScreen, close } = useModal();

  useEffect(() => {
    openScreen({
      id: "1",
      title: "Lorem ipsum dolor sit amet",
      component: (
        <div className="flex flex-row gap-3">
          <img src="https://picsum.photos/id/58/400/400" alt="Placeholder" />
          <div className="flex flex-col gap-4 px-8 py-12">
            <h1 className="text-display-sm">Lorem ipsum dolor sit amet</h1>
            <p className="text-body-md">
              Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do
              eiusmod tempor incididunt ut labore et dolore magna aliqua.
            </p>
            <p>
              Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do
              eiusmod tempor incididunt ut labore et dolore magna aliqua.
            </p>
            <p>
              Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do
              eiusmod tempor incididunt ut labore et dolore magna aliqua.
            </p>

            <div>
              <Button size="sm" onClick={close}>
                Done
              </Button>
            </div>
          </div>
        </div>
      ),
    });
  }, [openScreen, close]);

  return (
    <div>
      <Underlay />
      <Modal
        closable
        layout="custom"
        className="min-h-auto max-w-[800px] min-w-1/2 rounded-none p-0"
      />
    </div>
  );
};

export const CustomLayout: Story = {
  render: () => {
    return (
      <ModalProvider>
        <CustomModal />
      </ModalProvider>
    );
  },
};
