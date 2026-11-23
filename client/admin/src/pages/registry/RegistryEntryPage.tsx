import type { JSX } from "react";
import { useNavigate, useParams } from "@tanstack/react-router";
import { RegistryEntryEditor } from "./RegistryEntryEditor";

// Command K intentionally links the MCP Registry list, not these internal
// mutation forms. Staff discover entries with the list's server-side search
// and publication filters; promoting the editor from a sheet does not add a
// global registry index to the launcher. Covered by the palette page-action
// test and RegistryList search/link tests.
// The editor confirms its own discard, so leaving through Cancel skips the
// blocker rather than asking twice. Both pages keep the list's filters.
function useBackToList(): () => void {
  const navigate = useNavigate();
  return () =>
    void navigate({
      to: "/registry",
      search: (previous) => previous,
      ignoreBlocker: true,
    });
}

export function RegistryEntryPage(): JSX.Element {
  const { entryId } = useParams({ from: "/registry/$entryId" });
  const close = useBackToList();
  return <RegistryEntryEditor key={entryId} id={entryId} onClose={close} />;
}

export function NewRegistryEntryPage(): JSX.Element {
  const navigate = useNavigate();
  const close = useBackToList();
  return (
    <RegistryEntryEditor
      id={null}
      onClose={close}
      // A created entry is clean, so the blocker has nothing to protect.
      onCreated={(entryId) =>
        void navigate({
          to: "/registry/$entryId",
          params: { entryId },
          search: (previous) => previous,
          replace: true,
          ignoreBlocker: true,
        })
      }
    />
  );
}
