import { cn } from "@/lib/utils";
import { SpeakeasyLogoHorizontal } from "./variants/horizontal";
import { SpeakeasyIcon } from "./variants/icon";
import { SpeakeasyLogoVertical } from "./variants/vertical";

export const SpeakeasyLogo = ({
  variant = "horizontal",
  className,
}: {
  variant?: "horizontal" | "vertical" | "icon";
  className?: string;
}): JSX.Element => {
  const variantsMap = {
    horizontal: SpeakeasyLogoHorizontal,
    vertical: SpeakeasyLogoVertical,
    icon: SpeakeasyIcon,
  };
  return (
    <div
      role="img"
      aria-label="Speakeasy"
      className={cn("flex items-center dark:invert", className)}
    >
      {variantsMap[variant]()}
    </div>
  );
};
