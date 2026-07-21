import { Button, Icon, type ButtonProps } from "@speakeasy-api/moonshine";
import { forwardRef } from "react";

type PluginInstallButtonProps = Omit<ButtonProps, "children" | "variant">;

export const PluginInstallButton = forwardRef<
  HTMLButtonElement,
  PluginInstallButtonProps
>(function PluginInstallButton(buttonProps, ref) {
  return (
    <Button ref={ref} variant="primary" {...buttonProps}>
      <Button.Text>Install</Button.Text>
      <Button.Icon
        aria-hidden="true"
        className="bg-primary-foreground/25 h-4 w-px self-center"
      />
      <Button.RightIcon>
        <Icon aria-hidden="true" name="chevron-down" className="h-4 w-4" />
      </Button.RightIcon>
    </Button>
  );
});
