import { MoreActions } from "@/components/ui/more-actions";
import {
  SourceToolActionsProps,
  useSourceToolActions,
} from "./useSourceToolActions";

export function SourceToolActions(props: SourceToolActionsProps): JSX.Element {
  const { actions, dialog } = useSourceToolActions(props);

  return (
    <>
      <MoreActions actions={actions} />
      {dialog}
    </>
  );
}
