import { Link } from "react-router";
import { useSetup } from "./setup-context";
import { Fragment, type ReactNode } from "react";
import { SquareCheck, SquareDashed } from "lucide-react";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { names, ready } from "./state";
import { Correction } from "./shared";
function LiveConnectionError() {
  const { live, dispatch } = useSetup();
  return (
    <>
      <p>{live?.error}</p>
      {live?.recovery === "signin" && (
        <Button
          variant="secondary"
          size="sm"
          onClick={() => dispatch({ type: "modal", modal: "signin" })}
        >
          <Button.Text>Sign in again ↗</Button.Text>
        </Button>
      )}
      {live?.recovery === "credentials" && (
        <Correction step="credentials">Review credentials</Correction>
      )}
      {live?.recovery === "permissions" && (
        <Correction step="app">Review Slack permissions</Correction>
      )}
      {live?.recovery === "settings" && (
        <Link to={live.settingsHref ?? "../settings#authentication"}>
          Open identity settings
        </Link>
      )}
    </>
  );
}

export function ConnectionTask(): JSX.Element {
  const { s, dispatch, live } = useSetup();
  const available = ready(s);
  type Row = {
    label: string;
    done: boolean;
    status: string;
    action: string;
    available: boolean;
    step?: string;
    modal?: string;
    hint?: string;
    error?: ReactNode;
  };
  const rows: Row[] = [
    {
      label: "App credentials",
      done: s.saved,
      status: "Saved",
      action: "Enter credentials",
      available: true,
      step: "credentials",
    },
    {
      label: "Slack sign-in",
      done: s.authorized,
      status: "Signed in",
      action:
        live?.error && live.errorStep === "signin"
          ? "Try again ↗"
          : "Sign in ↗",
      available,
      modal: "signin",
      hint: "Save your connection credentials first.",
      error:
        live?.error && live.errorStep === "signin" ? (
          <LiveConnectionError />
        ) : undefined,
    },
    {
      label: "Connection test",
      done: s.tested,
      status: "Passed",
      action:
        live?.error && live.errorStep === "test"
          ? "Test again"
          : "Test connection",
      available: available && s.authorized,
      modal: "test",
      hint: "Sign in to Slack first.",
      error:
        live?.error && live.errorStep === "test" ? (
          <LiveConnectionError />
        ) : undefined,
    },
  ];
  return (
    <>
      <table
        className="w-full border-collapse [&_tr>td:first-child]:pl-4"
        aria-label={`${names[s.provider]} connection actions`}
      >
        <tbody>
          {rows.map((row, index) => {
            const Icon = row.done ? SquareCheck : SquareDashed;
            return (
              <Fragment key={row.label}>
                <tr className={index === rows.length - 1 ? "" : "border-b"}>
                  <td
                    className={`py-8 pr-3 align-top ${index === 0 ? "pt-0" : ""} ${index === rows.length - 1 && !row.error ? "pb-0" : ""}`}
                  >
                    <div className="flex min-h-9 items-center gap-2.5 py-1.5 text-base">
                      <Icon
                        aria-hidden
                        className={`size-6 shrink-0 ${row.done ? "text-default-success" : "text-muted-foreground"}`}
                      />
                      <span>{row.label}</span>
                    </div>
                    {!row.done && !row.available && (
                      <p className="text-[13px] text-muted-foreground lg:ml-[34px]">
                        {row.hint}
                      </p>
                    )}
                  </td>
                  <td
                    className={`py-8 text-right align-top ${index === 0 ? "pt-0" : ""} ${index === rows.length - 1 && !row.error ? "pb-0" : ""}`}
                  >
                    {row.done ? (
                      <span className="flex h-9 items-center justify-end text-[13px] leading-none text-default-success">
                        {row.status}
                      </span>
                    ) : (
                      <Button
                        variant="secondary"
                        disabled={!row.available || !!live?.pending}
                        onClick={() =>
                          row.step
                            ? dispatch({
                                type: "goto",
                                step: row.step,
                                correction: true,
                              })
                            : dispatch({ type: "modal", modal: row.modal! })
                        }
                      >
                        {live?.pending === "signin" && row.modal === "signin"
                          ? `Waiting for ${names[s.provider]}…`
                          : live?.pending === "test" && row.modal === "test"
                            ? "Testing…"
                            : row.action}
                      </Button>
                    )}
                    {row.modal === "signin" && live?.pending === "signin" && (
                      <Button variant="tertiary" onClick={live.cancelSignIn}>
                        <Button.Text>Cancel sign-in</Button.Text>
                      </Button>
                    )}
                  </td>
                </tr>
                {row.error && (
                  <tr>
                    <td colSpan={2} className="pb-8">
                      <Alert
                        variant="error"
                        className="[&_p]:mb-2.5 [&_p:last-child]:mb-0"
                      >
                        {row.error}
                      </Alert>
                    </td>
                  </tr>
                )}
              </Fragment>
            );
          })}
          {live?.toolCount != null && s.tested && (
            <tr>
              <td colSpan={2} className="pt-5 text-sm text-muted-foreground">
                Connected · {live.toolCount} tools discovered. No tools were
                run.
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </>
  );
}
