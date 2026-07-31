import type { Meta, StoryObj } from "@storybook/react-vite";

import { Modal } from ".";
import { Button } from "@/components/ui/Button";
import { ModalProvider } from "@/components/ui/context/ModalContext";
import { useModal } from "@/components/ui/hooks/useModal";

const meta: Meta<typeof Modal> = {
  title: "Design System/Modal",
  component: Modal,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Modal>;

function OpenModalButton() {
  const { openScreen } = useModal();

  return (
    <Button
      onClick={() =>
        openScreen({
          id: "welcome",
          title: "Connect a server",
          component: (
            <p className="text-sm">
              Paste an OpenAPI document or point Gram at a remote MCP server.
            </p>
          ),
        })
      }
    >
      Open modal
    </Button>
  );
}

export const Default: Story = {
  render: () => (
    <ModalProvider>
      <OpenModalButton />
      <Modal closable layout="default" />
    </ModalProvider>
  ),
};
