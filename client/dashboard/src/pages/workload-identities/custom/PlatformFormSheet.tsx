import {
  formatProblem,
  descriptionProblem,
  nameProblem,
  type RegisterIssuerValues,
} from "../formValues";
import { issuerValuesDiffer } from "../issuerEdit";
import { httpsUrlProblem } from "../issuerUrl";
import { tagsProblem } from "../tagLimits";
import type { InputField } from "./definition";
import { FormInput, FormTags } from "./FormBlocks";
import { FormSheet } from "./FormSheet";
import { useSheetForm } from "./useSheetForm";

interface PlatformFormSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /**
   * Called with the submitted values and, when editing, the issuer's values as
   * they stood when the sheet opened, which is what the edit is diffed against.
   */
  onSubmit: (
    values: RegisterIssuerValues,
    baseline: RegisterIssuerValues | undefined,
  ) => void;
  isPending: boolean;
  /**
   * A registered issuer's current values. When set, the sheet edits that
   * issuer: the form opens prefilled and the issuer URL is read-only, because
   * the server fixes it at registration.
   */
  initial?: RegisterIssuerValues;
}

const EMPTY: RegisterIssuerValues = {
  name: "",
  description: "",
  issuer: "",
  jwksUri: "",
  tags: [],
};

function platformValues(
  initial: RegisterIssuerValues | undefined,
): RegisterIssuerValues {
  return initial ?? EMPTY;
}

type PlatformTextField = Exclude<keyof RegisterIssuerValues, "tags">;

/** The value each input edits, and its id on the page. */
const PLATFORM_INPUTS: Partial<
  Record<InputField, { key: PlatformTextField; id: string }>
> = {
  name: { key: "name", id: "workload-issuer-name" },
  description: { key: "description", id: "workload-issuer-description" },
  issuer: { key: "issuer", id: "workload-issuer-url" },
  jwks_uri: { key: "jwksUri", id: "workload-issuer-jwks" },
};

/** Registers a trusted platform by hand, or edits one already registered. */
export function PlatformFormSheet({
  open,
  onOpenChange,
  onSubmit,
  isPending,
  initial,
}: PlatformFormSheetProps): JSX.Element {
  const isEditing = initial !== undefined;
  const { values, setValues, baseline } = useSheetForm(
    open,
    initial,
    platformValues,
  );

  const hasChanges =
    baseline === undefined || issuerValuesDiffer(baseline, values);

  const canSubmit =
    hasChanges &&
    values.name.trim().length > 0 &&
    nameProblem(values.name) === null &&
    values.issuer.trim().length > 0 &&
    values.jwksUri.trim().length > 0 &&
    httpsUrlProblem(values.issuer, true) === null &&
    httpsUrlProblem(values.jwksUri, false) === null &&
    descriptionProblem(values.description) === null &&
    tagsProblem(values.tags) === null;

  return (
    <FormSheet
      open={open}
      onOpenChange={onOpenChange}
      flow={isEditing ? "editPlatform" : "registerPlatform"}
      canSubmit={canSubmit}
      isPending={isPending}
      onSubmit={() => onSubmit(values, baseline)}
      renderers={{
        input: (block) => {
          const input = PLATFORM_INPUTS[block.field];
          if (input === undefined) return null;
          const value = values[input.key];
          return (
            <FormInput
              block={block}
              id={input.id}
              value={value}
              problem={formatProblem(block.format, value)}
              onChange={(next) => setValues({ ...values, [input.key]: next })}
            />
          );
        },
        tags: (block) => (
          <FormTags
            block={block}
            id="workload-issuer-tags"
            tags={values.tags}
            onChange={(tags) => setValues({ ...values, tags })}
          />
        ),
      }}
    />
  );
}
