import { cn } from "@/lib/utils";
import { Provider, useContext } from "./context";

type FrameProps = {
  children: React.ReactNode;
  className?: string;
};

/* Layout component for steps */
const Frame = ({ children, className }: FrameProps) => {
  return (
    <div className={cn("flex flex-col gap-y-8", className)}>{children}</div>
  );
};

export default {
  useContext,
  Provider,
  Frame,
};
