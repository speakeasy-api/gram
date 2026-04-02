import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Icon } from "@speakeasy-api/moonshine";
import { CheckIcon, LoaderCircleIcon, XIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

type StepStatus = "pending" | "running" | "done";

type Step = {
  label: string;
  status: StepStatus;
  progress: number; // 0-100
};

const INITIAL_STEPS: Step[] = [
  { label: "Analyzing your input...", status: "pending", progress: 0 },
  {
    label: "Building documentation structure...",
    status: "pending",
    progress: 0,
  },
  { label: "Generating page content...", status: "pending", progress: 0 },
  { label: "Applying your branding", status: "pending", progress: 0 },
];

// Simulated duration per step in ms
const STEP_DURATION = 2500;

export function AddRepoDialog({
  open,
  onOpenChange,
  onComplete,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onComplete: () => void;
}) {
  const [repoUrl, setRepoUrl] = useState("");
  const [phase, setPhase] = useState<"input" | "processing">("input");
  const [steps, setSteps] = useState<Step[]>(INITIAL_STEPS);
  const cancelledRef = useRef(false);
  const animFrameRef = useRef<number>(0);

  const reset = useCallback(() => {
    setRepoUrl("");
    setPhase("input");
    setSteps(INITIAL_STEPS);
    cancelledRef.current = false;
    cancelAnimationFrame(animFrameRef.current);
  }, []);

  // Reset when dialog closes
  useEffect(() => {
    if (!open) reset();
  }, [open, reset]);

  const handleStart = () => {
    if (!repoUrl.trim()) return;
    cancelledRef.current = false;
    setPhase("processing");
    runSteps();
  };

  const handleCancel = () => {
    cancelledRef.current = true;
    cancelAnimationFrame(animFrameRef.current);
    onOpenChange(false);
  };

  const runSteps = () => {
    let currentStep = 0;
    let stepStartTime = performance.now();

    const tick = (now: number) => {
      if (cancelledRef.current) return;

      const elapsed = now - stepStartTime;
      const progress = Math.min(100, (elapsed / STEP_DURATION) * 100);

      setSteps((prev) =>
        prev.map((s, i) => {
          if (i < currentStep) return { ...s, status: "done", progress: 100 };
          if (i === currentStep) return { ...s, status: "running", progress };
          return s;
        }),
      );

      if (progress >= 100) {
        currentStep++;
        stepStartTime = now;
        if (currentStep >= INITIAL_STEPS.length) {
          // All done — mark last step done
          setSteps((prev) =>
            prev.map((s) => ({ ...s, status: "done", progress: 100 })),
          );
          // Short delay then complete
          setTimeout(() => {
            if (!cancelledRef.current) {
              onComplete();
              onOpenChange(false);
            }
          }, 600);
          return;
        }
      }

      animFrameRef.current = requestAnimationFrame(tick);
    };

    animFrameRef.current = requestAnimationFrame(tick);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="sm:max-w-md">
        {phase === "input" ? (
          <>
            <Dialog.Header>
              <Dialog.Title>Add GitHub Repository</Dialog.Title>
              <Dialog.Description>
                Enter a GitHub repository URL to import documentation content.
              </Dialog.Description>
            </Dialog.Header>
            <div className="py-2">
              <Input
                placeholder="https://github.com/org/repo"
                value={repoUrl}
                onChange={setRepoUrl}
                onEnter={handleStart}
              />
            </div>
            <Dialog.Footer>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button onClick={handleStart} disabled={!repoUrl.trim()}>
                <Icon name="git-branch" className="h-4 w-4 mr-1.5" />
                Import
              </Button>
            </Dialog.Footer>
          </>
        ) : (
          <>
            <Dialog.Header>
              <Dialog.Title>Importing Repository</Dialog.Title>
              <Dialog.Description>
                <span className="font-mono text-xs">{repoUrl}</span>
              </Dialog.Description>
            </Dialog.Header>
            <div className="py-4 space-y-4">
              {steps.map((step, i) => (
                <StepRow key={i} step={step} />
              ))}
            </div>
            <Type small muted className="text-center block">
              The documentation process typically takes 15-30 minutes to
              complete. Feel free to close the tab. We'll email you when it's
              done.
            </Type>
            <Dialog.Footer>
              <Button variant="outline" onClick={handleCancel}>
                <XIcon className="h-4 w-4 mr-1.5" />
                Cancel
              </Button>
            </Dialog.Footer>
          </>
        )}
      </Dialog.Content>
    </Dialog>
  );
}

function StepRow({ step }: { step: Step }) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-2">
        <StepIcon status={step.status} />
        <span
          className={cn(
            "text-sm",
            step.status === "pending"
              ? "text-muted-foreground"
              : "text-foreground",
          )}
        >
          {step.label}
        </span>
      </div>
      <div className="ml-6 h-1.5 rounded-full bg-muted overflow-hidden">
        <div
          className={cn(
            "h-full rounded-full transition-all duration-150",
            step.status === "done" ? "bg-emerald-500" : "bg-primary",
          )}
          style={{ width: `${step.progress}%` }}
        />
      </div>
    </div>
  );
}

function StepIcon({ status }: { status: StepStatus }) {
  switch (status) {
    case "pending":
      return (
        <div className="h-4 w-4 rounded-full border border-muted-foreground/30" />
      );
    case "running":
      return <LoaderCircleIcon className="h-4 w-4 text-primary animate-spin" />;
    case "done":
      return <CheckIcon className="h-4 w-4 text-emerald-500" />;
  }
}
