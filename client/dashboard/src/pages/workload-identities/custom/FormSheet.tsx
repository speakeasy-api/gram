import { InlineEmptyState } from "@/components/inline-empty-state";
import {
  BlockList,
  type BlockRenderers,
} from "@/components/setup-steps/blockRegistry";
import { genericBlockRenderers } from "@/components/setup-steps/genericBlocks";
import { SetupSteps } from "@/components/setup-steps/SetupSteps";
import { Button } from "@/components/ui/Button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import type { ReactNode } from "react";
import type { CustomFlows, CustomForm, FormBlock } from "./definition";
import { useCustomFlows } from "./flows";

type ValueBlockRenderers = Omit<BlockRenderers<FormBlock>, "text" | "link">;

/**
 * Renderers for the blocks that read or change the form's values. A block a
 * form has no renderer for renders nothing.
 */
type FormBlockRenderers = Partial<ValueBlockRenderers>;

const NO_FORM_BLOCKS: ValueBlockRenderers = {
  input: () => null,
  agent_picker: () => null,
  tags: () => null,
  wildcard_caution: () => null,
};

interface FormSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Which custom flow to show. */
  flow: keyof CustomFlows;
  renderers: FormBlockRenderers;
  canSubmit: boolean;
  isPending: boolean;
  /** Called on submit while canSubmit holds and nothing is pending. */
  onSubmit: () => void;
}

const SHEET_CONTENT =
  "flex w-[560px] max-w-[calc(100vw-2rem)] flex-col sm:max-w-[560px]";

/**
 * One custom flow as a form in a sheet: its title and description, its single
 * step's blocks, and Cancel and submit. The values, their checks and what a
 * submit sends belong to the caller, through the renderers and callbacks.
 */
export function FormSheet({
  open,
  onOpenChange,
  flow,
  renderers,
  canSubmit,
  isPending,
  onSubmit,
}: FormSheetProps): JSX.Element {
  const { flows, isError, refetch } = useCustomFlows();
  const form = flows?.[flow];

  let body: ReactNode;
  if (form !== undefined) {
    body = (
      <FormBody
        form={form}
        renderers={renderers}
        canSubmit={canSubmit}
        isPending={isPending}
        onSubmit={onSubmit}
        onCancel={() => onOpenChange(false)}
      />
    );
  } else if (isError) {
    body = (
      <FormStatus title="Form unavailable">
        <InlineEmptyState
          icon="triangle-alert"
          heading="Couldn't load this form"
          description="The form failed to load. Try again in a moment."
          action={
            <Button size="sm" variant="secondary" onClick={refetch}>
              <Button.Text>Try again</Button.Text>
            </Button>
          }
        />
      </FormStatus>
    );
  } else {
    body = (
      <FormStatus title="Loading form">
        <SkeletonParagraph lines={6} />
      </FormStatus>
    );
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className={SHEET_CONTENT}>
        {body}
      </SheetContent>
    </Sheet>
  );
}

function FormBody({
  form,
  renderers,
  canSubmit,
  isPending,
  onSubmit,
  onCancel,
}: {
  form: CustomForm;
  renderers: FormBlockRenderers;
  canSubmit: boolean;
  isPending: boolean;
  onSubmit: () => void;
  onCancel: () => void;
}): JSX.Element {
  const allRenderers: BlockRenderers<FormBlock> = {
    ...NO_FORM_BLOCKS,
    ...genericBlockRenderers,
    ...renderers,
  };

  const handleSubmit: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    if (!canSubmit || isPending) return;
    onSubmit();
  };

  const footer = (
    <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
      <Button
        type="button"
        variant="secondary"
        onClick={onCancel}
        disabled={isPending}
      >
        <Button.Text>Cancel</Button.Text>
      </Button>
      <Button
        type="submit"
        variant="primary"
        disabled={!canSubmit || isPending}
      >
        <Button.Text>
          {isPending ? form.pendingLabel : form.submitLabel}
        </Button.Text>
      </Button>
    </SheetFooter>
  );

  return (
    <>
      <SheetHeader className="px-6 pt-6 pb-0">
        <SheetTitle className="text-lg font-semibold">{form.title}</SheetTitle>
        <SheetDescription>{form.description}</SheetDescription>
      </SheetHeader>

      <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col">
        <SetupSteps
          steps={form.steps}
          activeStepId={form.steps[0]?.id ?? ""}
          onStepChange={noStepChange}
          renderStep={(step) => (
            <BlockList
              blocks={step.blocks}
              renderers={allRenderers}
              keyPrefix={step.id}
            />
          )}
          footer={footer}
        />
      </form>
    </>
  );
}

/** A custom flow has a single step, so there is nowhere to move to. */
function noStepChange(): void {}

/** The sheet while its form is not there to show, titled for screen readers. */
function FormStatus({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <>
      <SheetHeader className="sr-only">
        <SheetTitle>{title}</SheetTitle>
      </SheetHeader>
      <div className="px-6 pt-12">{children}</div>
    </>
  );
}
