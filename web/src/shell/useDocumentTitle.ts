import { useEffect } from "react";
import { useTranslation } from "react-i18next";

export function useDocumentTitle(title: string) {
  const { t } = useTranslation();
  useEffect(() => {
    document.title = `${title} · ${t("app.name")}`;
  }, [title, t]);
}
