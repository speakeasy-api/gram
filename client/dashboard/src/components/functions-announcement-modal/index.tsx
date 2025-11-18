import {
  Button,
  Heading,
  Text,
  useModal,
  Link,
  useMoonshineConfig,
} from "@speakeasy-api/moonshine";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";
import Image from "./gram-functions.webp";
import ImageDark from "./gram-functions-dark.webp";
import { Badge } from "../ui/badge";
import { useNavigate } from "react-router";
import { useSlugs } from "@/contexts/Sdk";

const LOCAL_STORAGE_KEY =
  "gram-dashboard-has-seen-functions-announcement-modal";

export function FunctionsAnnouncementModal({
  onClose,
}: {
  onClose: () => void;
}) {
  const { close } = useModal();
  const [, setHasSeenFunctionsModal] = useLocalStorageState(
    LOCAL_STORAGE_KEY,
    false,
  );
  const { theme } = useMoonshineConfig();

  const navigate = useNavigate();
  const handleClose = () => {
    setHasSeenFunctionsModal(true);
    close();
    onClose();
  };

  const { orgSlug, projectSlug } = useSlugs();
  const goToFunctions = () => {
    setHasSeenFunctionsModal(true);
    navigate(
      `/${orgSlug}/${projectSlug}/onboarding?start-path=cli&start-step=cli-setup`,
    );
  };

  return (
    <div className="flex flex-row p-6 w-full h-full">
      <div className="h-full p-6">
        <div className="flex flex-col gap-3">
          <Heading className="whitespace-nowrap">
            Introducing Gram Functions
            <Badge
              variant="outline"
              className="relative -right-2 -top-2 text-xs py-1 px-2"
            >
              New
            </Badge>
          </Heading>
          <Text className="leading-6">
            Gram Functions enable you to compose agent tools in TypeScript and
            deploy them as MCP servers on Gram's serverless infrastructure.
          </Text>

          <Text className="leading-6">
            Now you can extend your agents tools beyond OpenAPI: take advantage
            of the NPM ecosystem, wrap multiple APIs, or query a database.
          </Text>
          <div className="flex flex-row gap-3 mt-4">
            <Button variant="brand" onClick={goToFunctions}>
              Try Gram Functions
            </Button>
            <Button onClick={handleClose} variant="tertiary">
              Don't show again
            </Button>
          </div>
        </div>
      </div>
      <div className="hidden [@media(min-width:1200px)]:block h-full p-5">
        <img
          src={theme === "dark" ? ImageDark : Image}
          alt="Gram Functions"
          className="md:max-w-[425px] 2xl:max-w-[450px]"
        />
      </div>
    </div>
  );
}
