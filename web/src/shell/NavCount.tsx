import { useTranslation } from "react-i18next";
import { useShellStatus } from "../app/shellStatus";

/** Sidebar count badge. Renders nothing while the count is unknown or zero, never a fake "0". */
export function NavCount({ kind }: { kind: "jobs" | "attention" }) {
  const { t } = useTranslation();
  const status = useShellStatus();
  const count = kind === "jobs" ? status.jobs : status.attention;
  if (count === undefined || count <= 0) return null;
  const label = kind === "jobs" ? t("shell.jobsCount", { count }) : t("shell.attentionCount", { count });
  return (
    <span className={`hx-count hx-count--${kind}`} aria-label={label} title={label}>
      <span aria-hidden="true">{count}</span>
    </span>
  );
}
