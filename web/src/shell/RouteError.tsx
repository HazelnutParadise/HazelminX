import { Button } from "@cloudflare/kumo/components/button";
import { CopyIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { isRouteErrorResponse, useLocation, useRouteError } from "react-router";
import { ApiError } from "../api/client";
import { pageForPath } from "../app/navigation";
import { RawMessage } from "../components/RawMessage";
import { PageHead } from "./PageShell";
import { StateBlock } from "./PageStates";

/** Page load failure inside the shell (board ShellStates): what failed, the trace ID, reload and copy. */
export function RouteError() {
  const { t } = useTranslation();
  const error = useRouteError();
  const { pathname } = useLocation();
  const [copied, setCopied] = useState(false);
  const page = pageForPath(pathname);

  const api = error instanceof ApiError ? error : null;
  const summary = api
    ? [t("crash.coreStatus", { status: api.status }), api.traceId && t("crash.trace", { trace: api.traceId })]
        .filter(Boolean)
        .join(" · ")
    : t("crash.frontend");
  const raw = api
    ? api.raw || api.message
    : error instanceof Error
      ? `${error.name}: ${error.message}${error.stack ? `\n${error.stack}` : ""}`
      : isRouteErrorResponse(error)
        ? `${error.status} ${error.statusText}`
        : String(error);

  const copyTrace = async () => {
    if (!api?.traceId) return;
    try {
      await navigator.clipboard.writeText(api.traceId);
      setCopied(true);
    } catch {
      // Clipboard can be blocked; the trace ID stays visible in the pill.
    }
  };

  return (
    <div className="hx-crash">
      {page && <PageHead title={t(`nav.${page}`)} />}
      <StateBlock
        heading={!page}
        title={t("crash.title")}
        detail={<RawMessage summary={summary} raw={raw} source={api ? "HazelminX Core" : "HazelminX Web"} />}
        actions={
          <>
            <Button variant="primary" onClick={() => window.location.reload()}>
              {t("crash.reload")}
            </Button>
            {api?.traceId && (
              <Button variant="secondary" icon={<CopyIcon aria-hidden="true" />} onClick={() => void copyTrace()}>
                {copied ? t("crash.copied") : t("crash.copyTrace")}
              </Button>
            )}
          </>
        }
      />
    </div>
  );
}
