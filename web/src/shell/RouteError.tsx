import { Button } from "@cloudflare/kumo/components/button";
import { useTranslation } from "react-i18next";
import { isRouteErrorResponse, useRouteError } from "react-router";
import { ApiError } from "../api/client";
import { ApiErrorMessage, RawMessage } from "../components/RawMessage";
import { PageHead } from "./PageShell";

/** Error boundary for pages: the shell stays usable and the original error is kept verbatim. */
export function RouteError() {
  const { t } = useTranslation();
  const error = useRouteError();
  const raw =
    error instanceof Error
      ? `${error.name}: ${error.message}${error.stack ? `\n${error.stack}` : ""}`
      : isRouteErrorResponse(error)
        ? `${error.status} ${error.statusText}`
        : String(error);

  return (
    <div className="hx-crash" role="alert">
      <PageHead
        title={t("crash.title")}
        actions={
          <Button variant="primary" onClick={() => window.location.reload()}>
            {t("crash.reload")}
          </Button>
        }
      />
      <section className="hx-panel hx-panel--pad">
        {error instanceof ApiError ? (
          <ApiErrorMessage error={error} />
        ) : (
          <RawMessage summary={t("crash.body")} raw={raw} source="HazelminX Web" />
        )}
      </section>
    </div>
  );
}
