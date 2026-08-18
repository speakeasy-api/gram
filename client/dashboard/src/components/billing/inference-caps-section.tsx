import { InferenceCapControl } from "@/components/billing/inference-cap-control";
import { sortInferenceCaps } from "@/components/billing/inference-caps";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useSession } from "@/contexts/Auth";
import { type ProductTier, useProductTier } from "@/hooks/useProductTier";
import { useTrialNow } from "@/hooks/useTrialNow";
import {
  getTrialLifecycleFromDates,
  type TrialLifecycle,
} from "@/lib/trial-status";
import { useGetInferenceSpendCaps } from "@gram/client/react-query/getInferenceSpendCaps.js";

/**
 * What the inference cap section does for this organization.
 *
 * The caps belong to the pay-as-you-go surfaces, so tiers with no PAYG bill for
 * a cap to apply to see nothing at all.
 *
 * An active trial takes precedence over the tier. Trials run on the enterprise
 * tier, but the account type can already read as PAYG while the trial is still
 * running. Either way the keys are live and their caps are enforced on the
 * trial's own defaults — so a trial gets the caps shown as locked rather than
 * hidden, and what it withholds is the ability to change them.
 */
type InferenceCapsMode = "editable" | "trial-locked" | "hidden";

function inferenceCapsMode(
  productTier: ProductTier,
  trialLifecycle: TrialLifecycle,
): InferenceCapsMode {
  if (productTier !== "payg" && productTier !== "enterprise") return "hidden";
  if (trialLifecycle === "active") return "trial-locked";
  // Enterprise off a trial is on a contract, which bills through its own terms.
  return productTier === "payg" ? "editable" : "hidden";
}

const TRIAL_NOTE =
  "These caps are enforced during your trial. You can change them once pay as you go begins.";

// The endpoint answers with the Gram-managed inference keys that have been
// materialized for this organization, which can be none of them.
const NO_CAPS_NOTE =
  "No Gram-managed inference keys are available to configure yet.";

/**
 * The monthly ceilings on the inference Gram runs for this organization — one
 * independent control per Gram-managed key it has.
 *
 * The tier rule lives here rather than at the call site so the billing page can
 * place the section in both of its branches without either one re-deriving when
 * the caps apply.
 */
export function InferenceCapsSection(): JSX.Element | null {
  const productTier = useProductTier();
  const { trial } = useSession();
  // A trial that ends while the page is open has to unlock the caps with it, so
  // this reads a clock that re-renders on the trial's own boundaries.
  const now = useTrialNow(trial);
  const mode = inferenceCapsMode(
    productTier,
    getTrialLifecycleFromDates(trial, now),
  );

  if (mode === "hidden") return null;

  return (
    <Page.Section>
      {/* Secondary section below Usage: suppress the area eyebrow. */}
      <Page.Section.Title area="">Inference caps</Page.Section.Title>
      <Page.Section.Description>
        Limit what this organization can spend each month on the inference Gram
        runs for it. Each cap is enforced on its own.
      </Page.Section.Description>
      <Page.Section.Body>
        <InferenceCaps
          trialNote={mode === "trial-locked" ? TRIAL_NOTE : null}
        />
      </Page.Section.Body>
    </Page.Section>
  );
}

/**
 * One control per Gram-managed inference key this organization has.
 *
 * The list is the whole story: it carries the materialized, undeleted keys Gram
 * manages, and the section renders exactly those — two, one, or none — rather
 * than a shape it assumed.
 *
 * `trialNote` is both halves of the trial lock: the reason it is read-only and
 * the fact that it is. There is one reason a cap can't be changed here, so the
 * note carrying it is what says the controls are locked.
 */
function InferenceCaps({
  trialNote,
}: {
  trialNote: string | null;
}): JSX.Element {
  // The shared query client throws everything but a 401/403 to the app error
  // boundary, which would take the whole billing page down over one section.
  // This section handles its own failures, so it opts out and keeps them inline.
  const { data, isError, refetch, isFetching } = useGetInferenceSpendCaps(
    undefined,
    undefined,
    { throwOnError: false },
  );

  // A refetch that fails leaves the last successful list in the cache, so the
  // query reports data and an error together. The controls stay mounted in the
  // same child positions — swapping them for the error message would throw away
  // whatever the admin had typed — and the failure is reported beside them.
  if (data) {
    const caps = sortInferenceCaps(data);

    return (
      <Stack gap={8}>
        {trialNote !== null && (
          <Text muted small>
            {trialNote}
          </Text>
        )}
        {caps.length === 0 ? (
          <Text muted small>
            {NO_CAPS_NOTE}
          </Text>
        ) : (
          caps.map((cap) => (
            <InferenceCapControl
              key={cap.keyType}
              cap={cap}
              locked={trialNote !== null}
            />
          ))
        )}
        {isError && (
          <Text muted small role="alert">
            Couldn't refresh the inference caps, so the amounts shown may be out
            of date. Saving still works.
          </Text>
        )}
      </Stack>
    );
  }

  // Nothing was ever cached, so there are no caps to show and no controls to
  // keep. The failure never reaches an error boundary, so recovery belongs
  // here: a retry of this one query rather than a reload of the billing page.
  if (isError) {
    return (
      <Stack direction="horizontal" align="center" gap={3}>
        <Text muted small role="alert">
          Couldn't load the inference caps.
        </Text>
        <Button
          variant="secondary"
          size="sm"
          disabled={isFetching}
          onClick={() => void refetch()}
        >
          {isFetching ? "RETRYING..." : "RETRY"}
        </Button>
      </Stack>
    );
  }

  return (
    <div className="max-w-md space-y-4">
      <Skeleton className="h-4 w-48" />
      <Skeleton className="h-9 w-full" />
      <Skeleton className="h-9 w-32" />
    </div>
  );
}
