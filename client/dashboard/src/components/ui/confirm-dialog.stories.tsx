import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { useConfirm } from "@/components/ui/use-confirm";
import { Button } from "@/components/ui/moonshine";

const meta: Meta<typeof ConfirmDialog> = {
  title: "UI/ConfirmDialog",
  component: ConfirmDialog,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof ConfirmDialog>;

function BasicExample(): React.JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="secondary" onClick={() => setOpen(true)}>
        <Button.Text>Leave toolset</Button.Text>
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title="Leave this toolset?"
        description="You can rejoin later from the toolset settings."
        onConfirm={() => setOpen(false)}
      />
    </>
  );
}

export const Basic: Story = {
  render: () => <BasicExample />,
};

function DestructiveExample(): React.JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="destructive-secondary" onClick={() => setOpen(true)}>
        <Button.Text>Delete API key</Button.Text>
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title="Delete API key"
        description="This will immediately revoke the key."
        impact={
          <ul className="list-disc pl-4">
            <li>Any integrations using this key stop working immediately</li>
            <li>Usage history for this key is retained for 30 days</li>
          </ul>
        }
        destructive
        confirmLabel="Delete"
        onConfirm={() => setOpen(false)}
      />
    </>
  );
}

export const Destructive: Story = {
  render: () => <DestructiveExample />,
};

function TypeToConfirmExample(): React.JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="destructive-primary" onClick={() => setOpen(true)}>
        <Button.Text>Delete project</Button.Text>
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title="Delete project"
        description="This permanently deletes the project and everything in it."
        impact={
          <ul className="list-disc pl-4">
            <li>All toolsets and deployments are removed</li>
            <li>API keys scoped to this project stop working</li>
          </ul>
        }
        destructive
        confirmLabel="Delete project"
        confirmValue="my-project"
        onConfirm={() => setOpen(false)}
      />
    </>
  );
}

export const TypeToConfirm: Story = {
  render: () => <TypeToConfirmExample />,
};

function PendingExample(): React.JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="secondary" onClick={() => setOpen(true)}>
        <Button.Text>Redeploy</Button.Text>
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title="Redeploy this environment?"
        description="Simulates an async action with a 1.5s delay before the dialog closes."
        confirmLabel="Redeploy"
        onConfirm={async () => {
          await new Promise((resolve) => {
            setTimeout(resolve, 1500);
          });
        }}
      />
    </>
  );
}

export const PendingState: Story = {
  render: () => <PendingExample />,
};

function ImperativeHookExample(): React.JSX.Element {
  const { confirm: requestConfirm, dialog } = useConfirm();
  const [result, setResult] = useState<string | null>(null);

  const handleClick = async (): Promise<void> => {
    const confirmed = await requestConfirm({
      title: "Discard unsaved changes?",
      description: "Your edits will be lost.",
      destructive: true,
      confirmLabel: "Discard",
    });
    setResult(confirmed ? "Confirmed" : "Canceled");
  };

  return (
    <div className="flex flex-col items-start gap-2">
      <Button variant="secondary" onClick={() => void handleClick()}>
        <Button.Text>Close without saving</Button.Text>
      </Button>
      {result && (
        <span className="text-muted-foreground font-mono text-xs tracking-[0.08em] uppercase">
          {result}
        </span>
      )}
      {dialog}
    </div>
  );
}

export const ImperativeHook: Story = {
  render: () => <ImperativeHookExample />,
};
