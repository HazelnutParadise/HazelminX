import { Empty } from "@cloudflare/kumo/components/empty";
import { useTranslation } from "react-i18next";
import { PAGES, type PageKey } from "../app/navigation";
import { useDocumentTitle } from "./useDocumentTitle";

/** Page heading row: h1 on the left, page actions on the right (DESIGN.md Layout「頁面標題列」). */
export function PageHead({ title, actions }: { title: string; actions?: React.ReactNode }) {
  return (
    <div className="hx-phead">
      <h1>{title}</h1>
      {actions && <div className="hx-phead-actions">{actions}</div>}
    </div>
  );
}

/** Route placeholder for pages whose tickets have not landed yet. */
export function PageShell({ page }: { page: PageKey }) {
  const { t } = useTranslation();
  const name = t(`nav.${page}`);
  useDocumentTitle(name);
  const Icon = PAGES[page].icon;
  return (
    <>
      <PageHead title={name} />
      <Empty className="hx-empty" icon={<Icon size={40} aria-hidden="true" />} title={t("page.emptyTitle", { name })} />
    </>
  );
}
