import { cn } from "@/lib/utils";
import { useContext } from "./context";

type FrameProps = {
  children: React.ReactNode;
};

export default function Frame({ children }: FrameProps) {
  const step = useContext();

  return (
    <div
      className={cn(
        "grid grid-cols-[auto_1fr]  gap-4 [grid-template-areas:'indicator_header'_'indicator_content'] transition-all duration-200",
        step.isCurrentStep ? "opacity-100" : "opacity-50",
      )}
    >
      {children}
    </div>
  );
}
