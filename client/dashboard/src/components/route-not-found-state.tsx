import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
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
}: RouteNotFoundStateProps): JSX.Element {
  return (
    <div className="flex min-h-[420px] w-full items-center justify-center">
      <Stack gap={4} align="center" className="max-w-md text-center">
        <Icon name="circle-alert" className="size-10" />
        <Stack gap={2} align="center">
          <Text variant="subheading">{title}</Text>
          <Text muted>{description}</Text>
        </Stack>
        {action}
      </Stack>
    </div>
  );
}

export function SecondaryRouteAction({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  return (
    <Button variant="secondary">
      <Button.Text>{children}</Button.Text>
    </Button>
  );
}
