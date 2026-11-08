import { useEffect, useState } from "react";
import { Button } from "@cloudflare/kumo/components/button";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { ArrowClockwiseIcon, LockIcon, ScrollIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import { activeLocale } from "../i18n";
import { formatElapsed } from "../lib/time";
import "./system.css";

/**
 * Banner for a stopped platform component (DESIGN.md「系統元件停止時」, ROB-013). SLOT — props are fixed.
 */
export interface ComponentDownBannerProps {
  /** Component name, e.g. "Zentinel". */
  name: string;
  kind: "systemd" | "container";
  /** Host name the component runs on. */
  host: string;
  /** UTC RFC 3339 time it stopped; the banner shows how long ago. */
  stoppedAt: string;
  /** Reference "now" for the elapsed time; defaults to the current time. */
  now?: Date;
  exitCode?: number;
  /** Raw reason from systemd or the engine, e.g. "OOMKilled"; shown verbatim in mono. */
  reason?: string;
  /** Features that do not work while it is stopped. */
  affected: string[];
  /** Last log lines before it stopped, shown verbatim below the banner. */
  logTail?: string[];
  /** When false the restart button is disabled with a lock and `noPermissionContact` explains whom to ask. */
  canRestart: boolean;
  noPermissionContact?: string;
  restarting?: boolean;
  /** Consecutive failed restarts and the last error (raw). */
  failures?: { count: number; lastError: string };
  onRestart: () => void;
  onViewLogs: () => void;
}

export function ComponentDownBanner({
  name,
  kind,
  host,
  stoppedAt,
  now,
  exitCode,
  reason,
  affected,
  logTail,
  canRestart,
  noPermissionContact,
  restarting = false,
  failures,
  onRestart,
  onViewLogs,
}: ComponentDownBannerProps) {
  const { t } = useTranslation();
  const [fallbackNow, setFallbackNow] = useState(() => new Date());
  useEffect(() => {
    if (now) return;
    // Without a reference time from the caller, keep the stopped duration current.
    const id = window.setInterval(() => setFallbackNow(new Date()), 30_000);
    return () => window.clearInterval(id);
  }, [now]);
  const currentLocale = activeLocale();
  const elapsed = formatElapsed(stoppedAt, now ?? fallbackNow, currentLocale);
  const duration = elapsed ?? t("unknown");

  const restartLabel = restarting ? t("componentDown.restarting") : t("componentDown.restart", { name });
  const isZh = currentLocale.startsWith("zh");
  const affectedSeparator = isZh ? "、" : ", ";
  const affectedColon = isZh ? "：" : ": ";

  return (
    <div className="hx-cdown" role="alert">
      <div className="hx-cdown-header">
        <div className="hx-cdown-content">
          <strong className="hx-cdown-title">{t("componentDown.title", { name, host, duration })}</strong>

          <div className="hx-cdown-row">
            <span className="hx-cdown-kind">{t(`componentDown.kind.${kind}`)}</span>

            {exitCode !== undefined && (
              <span className="hx-status-pill hx-status-pill--danger">
                <span className="hx-status-shape" aria-hidden="true">
                  ■
                </span>
                <span>{t("componentDown.exit", { code: exitCode })}</span>
              </span>
            )}

            {reason && <span className="hx-cdown-reason">{reason}</span>}

            {affected.length > 0 && (
              <span className="hx-cdown-affected">
                {t("componentDown.affected")}
                {affectedColon}
                {affected.join(affectedSeparator)}
              </span>
            )}
          </div>

          {failures && (
            <div className="hx-cdown-failures">
              <span>{t("componentDown.failures", { count: failures.count })}</span>
              <span className="hx-cdown-lasterror">{failures.lastError}</span>
            </div>
          )}
        </div>

        <div className="hx-cdown-actions">
          {canRestart ? (
            <Button
              variant="primary"
              icon={<ArrowClockwiseIcon aria-hidden="true" />}
              loading={restarting}
              onClick={onRestart}
            >
              {restartLabel}
            </Button>
          ) : (
            <Tooltip
              content={t("componentDown.noPermission", { name, who: noPermissionContact })}
              render={<span tabIndex={0} className="hx-cdown-lock-wrapper" />}
            >
              <Button variant="primary" disabled icon={<LockIcon aria-hidden="true" />}>
                {restartLabel}
              </Button>
            </Tooltip>
          )}

          <Button variant="secondary" icon={<ScrollIcon aria-hidden="true" />} onClick={onViewLogs}>
            {t("componentDown.viewLogs")}
          </Button>
        </div>
      </div>

      {logTail && logTail.length > 0 && (
        <pre className="hx-scroll hx-cdown-logtail" tabIndex={0} aria-label={t("componentDown.logTail")}>
          {logTail.join("\n")}
        </pre>
      )}
    </div>
  );
}
