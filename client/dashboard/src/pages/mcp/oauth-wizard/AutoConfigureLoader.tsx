import { Text } from "@/components/ui/Text";
import { Spinner } from "@/components/ui/Spinner";

export function AutoConfigureLoader(): JSX.Element {
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-12">
      <Spinner className="text-muted-foreground mr-0 h-12 w-12" />
      <Text className="text-center text-lg font-medium">
        Setting up OAuth Proxy...
      </Text>
      <Text muted small className="max-w-md text-center">
        Registering the platform with the upstream OAuth provider and storing
        the returned credentials.
      </Text>
    </div>
  );
}
