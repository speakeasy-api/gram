import { Button, type ButtonProps } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { forwardRef } from "react";

type PluginInstallButtonProps = Omit<ButtonProps, "children" | "variant"> & {
  loading?: boolean;
};

export const PluginInstallButton = forwardRef<
  HTMLButtonElement,
  PluginInstallButtonProps
>(function PluginInstallButton({ loading = false, ...buttonProps }, ref) {
  return (
    <Button ref={ref} variant="primary" {...buttonProps} aria-busy={loading}>
      <Button.Text>{loading ? "Downloading..." : "Install"}</Button.Text>
      <Button.Icon
        aria-hidden="true"
        className="bg-primary-foreground/25 h-4 w-px self-center"
      />
      <Button.RightIcon>
        <Icon
          aria-hidden="true"
          name={loading ? "loader-circle" : "chevron-down"}
          className={loading ? "h-4 w-4 animate-spin" : "h-4 w-4"}
        />
      </Button.RightIcon>
    </Button>
  );
});
