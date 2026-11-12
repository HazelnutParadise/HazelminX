import { Button } from "@cloudflare/kumo/components/button";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ShellStatusContext } from "../app/shellStatus";
import { authKeys } from "../auth/api";
import type { SessionState } from "../auth/session";
import { ApiErrorMessage } from "../components/RawMessage";
import { PageSkeleton, StateBlock } from "./PageStates";
import { TopBar } from "./TopBar";
import "./shell.css";

const SIGNED_OUT = { signedOut: true };

/**
 * What the shell shows before it knows who is signed in: the first-load skeleton (after 2 s), or, when Core
 * cannot be asked, the failure with its original response and a retry.
 */
export function SessionFallback({ state }: { state: SessionState }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  return (
    <ShellStatusContext.Provider value={SIGNED_OUT}>
      <div className="hx-shell">
        <TopBar />
        <main id="main" className="hx-main" tabIndex={-1}>
          {state.status === "error" ? (
            <StateBlock
              heading
              title={t("auth.sessionFailed")}
              detail={<ApiErrorMessage error={state.error} />}
              actions={
                <Button variant="primary" onClick={() => void client.refetchQueries({ queryKey: authKeys.session })}>
                  {t("crash.reload")}
                </Button>
              }
            />
          ) : (
            <PageSkeleton />
          )}
        </main>
      </div>
    </ShellStatusContext.Provider>
  );
}
