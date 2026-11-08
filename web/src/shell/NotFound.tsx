import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { useTranslation } from "react-i18next";
import { useLocation } from "react-router";
import { StateBlock } from "./PageStates";
import { openSearch } from "./searchEvents";
import { useDocumentTitle } from "./useDocumentTitle";

export function NotFound() {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  useDocumentTitle(t("notFound.title"));
  return (
    <>
      <StateBlock
        heading
        title={t("notFound.title")}
        detail={<span className="hx-mono hx-note">{pathname}</span>}
        actions={
          <>
            <LinkButton href="/" variant="primary">
              {t("notFound.back")}
            </LinkButton>
            <Button variant="secondary" onClick={openSearch}>
              {t("notFound.search")}
            </Button>
          </>
        }
      />
    </>
  );
}
