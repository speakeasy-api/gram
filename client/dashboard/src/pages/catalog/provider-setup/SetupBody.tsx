import { oktaGuide } from "./okta";
import { useEffect, useRef } from "react";
import { Link } from "react-router";
import { BookOpen, Check, ExternalLink } from "lucide-react";
import { SetupGuideLink } from "@/components/setup-guide/SetupGuideLink";
import { useConfettiBurst } from "@/components/icon-confetti";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Alert } from "@/components/ui/Alert";
import { useSetup } from "./setup-context";
import { names, ready, steps } from "./state";
import { ProviderTask } from "./ProviderTasks";
import { taskTitle } from "./provider-data";
import { ConnectionTask } from "./ConnectionTask";

function StepRail(): JSX.Element {
  const { s, dispatch } = useSetup();
  return (
    <nav
      aria-label={`${names[s.provider]} setup steps`}
      className="row-start-1 min-w-0 lg:sticky lg:top-6 lg:col-start-2 lg:row-start-1"
    >
      <ol className="flex flex-wrap gap-x-[18px] gap-y-3 lg:block">
        {steps(s).map((step, index) => {
          let completed = s.done.includes(step.id);
          if (step.id === "credentials") completed = s.saved;
          if (step.id === "test") completed = s.tested;
          return (
            <li
              key={step.id}
              className="relative text-xs lg:pb-3 lg:after:absolute lg:after:top-6 lg:after:bottom-0.5 lg:after:left-[11px] lg:after:w-px lg:after:bg-border lg:last:pb-0 lg:last:after:hidden"
            >
              <button
                type="button"
                className="flex items-center gap-3 text-left"
                aria-current={s.step === step.id ? "step" : undefined}
                onClick={() => dispatch({ type: "goto", step: step.id })}
              >
                <span
                  className={`relative z-1 grid size-6 shrink-0 place-items-center border font-mono text-[11px] ${s.step === step.id ? "border-foreground bg-foreground text-background" : "bg-card text-muted-foreground"}`}
                >
                  {completed ? (
                    <Check className="size-3" aria-hidden />
                  ) : (
                    index + 1
                  )}
                </span>
                <span className="text-sm">{step.title}</span>
              </button>
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
export function SetupBody({
  serverUrl,
  serverName,
}: {
  serverUrl: string;
  serverName: string;
}): JSX.Element {
  const { s, dispatch, live } = useSetup();
  const name = names[s.provider];
  const complete = ready(s) && s.authorized && s.tested;
  const canComplete =
    complete &&
    !live.pending &&
    !live.loading &&
    !live.error &&
    !live.completing;
  const { canvasRef, burst } = useConfettiBurst();
  // Initial validation is already complete when reopening; only celebrate a new pass.
  const celebratedValidation = useRef(complete);
  useEffect(() => {
    if (!complete) celebratedValidation.current = false;
    else if (
      s.step === "test" &&
      canComplete &&
      !celebratedValidation.current
    ) {
      celebratedValidation.current = true;
      burst({ particleCount: 45, ticks: 90 });
    }
  }, [complete, canComplete, s.step, burst]);
  let content = <ProviderTask serverName={serverName} />;
  if (s.step === "test") content = <ConnectionTask />;
  return (
    // On the Setup page, under the server's header: the current task's
    // heading, then the task beside its step rail.
    <div className="flex flex-col gap-6 [&_button:focus-visible]:outline-2 [&_button:focus-visible]:outline-offset-2 [&_button:focus-visible]:outline-[var(--border-focus)] [&_button:focus-visible]:outline-solid">
      <div className="grid grid-cols-1 items-end gap-6 lg:grid-cols-[minmax(0,7fr)_minmax(200px,3fr)] lg:gap-8 xl:gap-14">
        <div className="min-w-0">
          <Heading
            variant="h2"
            className="text-[28px] leading-9 tracking-[-.03em]"
          >
            {taskTitle(s)}
          </Heading>
        </div>
        {["snowflake", "okta"].includes(s.provider) ? (
          <Button
            variant="secondary"
            size="xs"
            className="bg-card mb-1 justify-self-start"
            asChild
          >
            <a
              href={
                s.provider === "okta"
                  ? oktaGuide
                  : "https://www.speakeasy.com/docs/ai-control-plane/guides/snowflake"
              }
              target="_blank"
              rel="noopener noreferrer"
            >
              <Button.LeftIcon>
                <BookOpen />
              </Button.LeftIcon>
              <Button.Text>Read the guide</Button.Text>
              <Button.RightIcon>
                <ExternalLink />
              </Button.RightIcon>
            </a>
          </Button>
        ) : (
          <SetupGuideLink
            serverUrl={serverUrl}
            variant="secondary"
            className="bg-card mb-1 justify-self-start"
          />
        )}
      </div>
      <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[minmax(0,7fr)_minmax(200px,3fr)] lg:gap-8 xl:gap-14">
        <section
          aria-label={`Current ${name} setup task`}
          className="@container/task relative isolate min-w-0 text-sm leading-[1.65] [&_summary]:cursor-pointer [&_strong]:font-semibold"
        >
          <canvas
            ref={canvasRef}
            hidden={s.step !== "test"}
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 -z-10 size-full"
          />
          {content}
          {live.error && (s.step !== "test" || live.errorStep === "save") && (
            <Alert variant="error" className="mt-5">
              <p>{live.error}</p>
              {live.recovery === "settings" ? (
                <Link to={live.settingsHref ?? "../settings#authentication"}>
                  Open identity settings
                </Link>
              ) : (
                live.loading && (
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={live.retryLoad}
                  >
                    <Button.Text>Retry loading saved credentials</Button.Text>
                  </Button>
                )
              )}
            </Alert>
          )}
          {s.step === "test" && live.completionError && (
            <Alert variant="error" className="mt-5">
              <p>{live.completionError}</p>
            </Alert>
          )}
          <>
            <div className="mt-[30px] flex justify-end gap-2 border-t pt-[22px]">
              {s.step !== "begin" && (
                <Button
                  variant="tertiary"
                  onClick={() => dispatch({ type: "back" })}
                >
                  <Button.Text>Back</Button.Text>
                </Button>
              )}
              {s.step === "test" ? (
                <Button
                  variant="primary"
                  disabled={!canComplete}
                  onClick={
                    live.complete ?? (() => dispatch({ type: "complete" }))
                  }
                >
                  <Button.Text>
                    {live.completing ? "Completing…" : "Complete setup"}
                  </Button.Text>
                </Button>
              ) : (
                <Button
                  variant="primary"
                  disabled={
                    !!live.pending ||
                    live.loading ||
                    (s.provider === "salesforce" &&
                      s.values.path === "support" &&
                      s.step === "support")
                  }
                  onClick={live.next}
                >
                  <Button.Text>
                    {live.pending === "save"
                      ? "Saving…"
                      : s.step === "begin"
                        ? "Get started"
                        : "Continue"}
                  </Button.Text>
                </Button>
              )}
            </div>
          </>
        </section>
        <StepRail />
      </div>
    </div>
  );
}
