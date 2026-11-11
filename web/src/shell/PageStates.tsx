import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

/**
 * Empty, not-found and failed pages share one block (board ShellStates): a dashed frame with a bold
 * sentence, optional detail and the page's actions.
 */
export function StateBlock({
  title,
  detail,
  actions,
  heading = false,
}: {
  title: string;
  detail?: ReactNode;
  actions?: ReactNode;
  /** Render the title as the page's h1 (pages that have no heading row of their own). */
  heading?: boolean;
}) {
  return (
    <section className="hx-state">
      {heading ? <h1 className="hx-state-title">{title}</h1> : <strong className="hx-state-title">{title}</strong>}
      {detail}
      {actions && <div className="hx-state-actions">{actions}</div>}
    </section>
  );
}

/** First-load placeholder. Hidden for the first 2 s so fast loads never flash a skeleton. */
export function PageSkeleton({ delayMs = 2000 }: { delayMs?: number }) {
  const { t } = useTranslation();
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const id = window.setTimeout(() => setShown(true), delayMs);
    return () => window.clearTimeout(id);
  }, [delayMs]);
  if (!shown) return null;
  return (
    <div className="hx-skeleton" role="status" aria-label={t("loading")}>
      {[40, 90, 75, 82].map((w, i) => (
        <span key={i} style={{ width: `${w}%`, height: i === 0 ? 20 : 12 }} />
      ))}
    </div>
  );
}
