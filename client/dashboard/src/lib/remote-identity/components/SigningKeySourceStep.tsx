import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { X } from "lucide-react";
import {
  AnimatePresence,
  LayoutGroup,
  motion,
  useReducedMotion,
} from "motion/react";
import { useId } from "react";
import {
  signingKeySourceOptions,
  type SigningKeySource,
} from "../model/serviceAccount";
import { AutoHeight } from "./AutoHeight";
import { AvailabilityBadge } from "./AvailabilityBadge";

const MORPH = { type: "spring", stiffness: 420, damping: 36 } as const;

/**
 * The "Create the client" step of the signing key setup. Its first decision is
 * where the key lives; once picked, that card morphs into a badge beside the
 * step's title, its siblings fade out, and the rest of the step fades in
 * underneath once the morph has landed. The region under the title springs to
 * its new height rather than jumping, so the steps below slide. The badge's X
 * runs it back. Under reduced motion it swaps without the morph.
 */
export function SigningKeySourceStep({
  title,
  marker,
  value,
  onChange,
  disabled = false,
  children,
}: {
  title: string;
  /** The step's number, or its done check. */
  marker: React.ReactNode;
  value: SigningKeySource | null;
  onChange: (value: SigningKeySource | null) => void;
  disabled?: boolean;
  /** The rest of the step, shown once a source is picked. */
  children: React.ReactNode;
}): JSX.Element {
  const group = useId();
  const reduced = useReducedMotion() ?? false;
  const options = signingKeySourceOptions();
  const chosen = options.find((option) => option.value === value) ?? null;
  const layoutId = (source: SigningKeySource) =>
    reduced ? undefined : `${group}-signing-key-${source}`;

  return (
    <LayoutGroup id={group}>
      <div className="flex gap-3">
        {marker}
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <div className="flex min-h-7 flex-wrap items-center gap-2">
            <Text small className="font-medium">
              {title}
            </Text>
            <AnimatePresence initial={false}>
              {chosen ? (
                <motion.span
                  key={chosen.value}
                  layoutId={layoutId(chosen.value)}
                  transition={reduced ? { duration: 0 } : MORPH}
                  className="border-primary bg-background inline-flex h-7 items-center gap-1 border pr-1 pl-2.5 text-[13px]"
                >
                  {chosen.title}
                  <button
                    type="button"
                    aria-label="Change signing key"
                    disabled={disabled}
                    onClick={() => onChange(null)}
                    className="text-muted-foreground hover:text-foreground inline-flex size-5 items-center justify-center disabled:cursor-not-allowed"
                  >
                    <X aria-hidden="true" className="size-3" />
                  </button>
                </motion.span>
              ) : null}
            </AnimatePresence>
          </div>

          <AutoHeight>
            <AnimatePresence initial={false} mode="popLayout">
              {chosen ? null : (
                <motion.div
                  key="choices"
                  initial={reduced ? false : { opacity: 0 }}
                  animate={{ opacity: 1 }}
                  exit={reduced ? undefined : { opacity: 0 }}
                  transition={{ duration: reduced ? 0 : 0.18 }}
                  className="flex flex-col gap-1.5"
                >
                  <Text muted small>
                    Signing key
                  </Text>
                  <RadioCardGroup
                    aria-label="Signing key"
                    size="sm"
                    orientation="horizontal"
                    value={null}
                    disabled={disabled}
                    onValueChange={(next) => onChange(next as SigningKeySource)}
                    className="grid-cols-1 sm:grid-cols-2"
                  >
                    {options.map((option) => (
                      <motion.div
                        key={option.value}
                        layoutId={layoutId(option.value)}
                        transition={reduced ? { duration: 0 } : MORPH}
                        className="flex"
                      >
                        <RadioCard
                          value={option.value}
                          disabled={option.comingSoon}
                          className="flex-1"
                          title={
                            <span className="flex flex-wrap items-center gap-2">
                              {option.title}
                              {option.comingSoon ? <AvailabilityBadge /> : null}
                            </span>
                          }
                        >
                          {option.description}
                        </RadioCard>
                      </motion.div>
                    ))}
                  </RadioCardGroup>
                </motion.div>
              )}
            </AnimatePresence>

            <AnimatePresence initial={false} mode="popLayout">
              {chosen ? (
                // Mounted at once so the height springs in one move; it only
                // fades in once the card has finished becoming the badge.
                <motion.div
                  key="form"
                  initial={reduced ? false : { opacity: 0, y: 6 }}
                  animate={{ opacity: 1, y: 0 }}
                  exit={reduced ? undefined : { opacity: 0 }}
                  transition={
                    reduced ? { duration: 0 } : { duration: 0.22, delay: 0.25 }
                  }
                  className="flex flex-col gap-3"
                >
                  {children}
                </motion.div>
              ) : null}
            </AnimatePresence>
          </AutoHeight>
        </div>
      </div>
    </LayoutGroup>
  );
}
