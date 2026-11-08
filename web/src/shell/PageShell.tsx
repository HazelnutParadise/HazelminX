import { useTranslation } from "react-i18next";
import type { PageKey } from "../app/navigation";
import { StateBlock } from "./PageStates";
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
  return (
    <>
      <PageHead title={name} />
      <StateBlock title={t("page.emptyTitle", { name })} />
    </>
  );
}
