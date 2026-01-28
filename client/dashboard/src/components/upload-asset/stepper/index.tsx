import { cn } from "@/lib/utils";
import { StepperContextProvider } from "./context";

export { useStepper } from "./context";

type FrameProps = {
  children: React.ReactNode;
  className?: string;
};

/* Layout component for steps */
const Frame = ({ children, className }: FrameProps) => {
  return (
    <div className={cn("flex flex-col gap-y-6", className)}>{children}</div>
  );
};

export default {
  Provider: StepperContextProvider,
  Frame,
};
