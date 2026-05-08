import { Type } from "@/components/ui/type";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import type { ReactNode } from "react";

type RouteNotFoundStateProps = {
  title: string;
  description: string;
  action: ReactNode;
};

export function RouteNotFoundState({
  title,
  description,
  action,
}: RouteNotFoundStateProps) {
  return (
    <div className="flex min-h-[420px] w-full items-center justify-center">
      <Stack gap={4} align="center" className="max-w-md text-center">
        <Icon name="circle-alert" className="size-10" />
        <Stack gap={2} align="center">
          <Type variant="subheading">{title}</Type>
          <Type muted>{description}</Type>
        </Stack>
        {action}
      </Stack>
    </div>
  );
}

export function SecondaryRouteAction({ children }: { children: ReactNode }) {
  return (
    <Button variant="secondary">
      <Button.Text>{children}</Button.Text>
    </Button>
  );
}
