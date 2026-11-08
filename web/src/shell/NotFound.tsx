import { LinkButton } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { CompassIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import { useLocation } from "react-router";
import { PageHead } from "./PageShell";
import { useDocumentTitle } from "./useDocumentTitle";

export function NotFound() {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  useDocumentTitle(t("notFound.title"));
  return (
    <>
      <PageHead title={t("notFound.title")} />
      <Empty
        className="hx-empty"
        icon={<CompassIcon size={40} aria-hidden="true" />}
        title={t("notFound.body", { path: pathname })}
        contents={
          <LinkButton href="/" variant="primary">
            {t("notFound.back")}
          </LinkButton>
        }
      />
    </>
  );
}
