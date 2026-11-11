import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { ArrowClockwiseIcon, FloppyDiskIcon } from "@phosphor-icons/react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useBlocker } from "react-router";
import "./system.css";

/**
 * System-wide unsaved-changes bar (DESIGN.md「存檔一定看得到」, board ShellStates): the count, what saving
 * causes, discard and a primary button that names the result. It also asks before the user leaves the page,
 * both for in-app navigation (留在這頁／不存，離開／儲存後離開) and for closing or reloading the tab.
 */
export interface SaveBarProps {
  /** Number of unsaved changes. 0 renders nothing. */
  changes: number;
  /** What is being edited, e.g. ".env". Optional; prefixes the count and names it in the leave dialog. */
  subject?: string;
  /** What saving will cause, e.g. "存檔後要重建 shop-api 才會生效". Optional. */
  effect?: string;
  /** Primary button text that names the result, e.g. "儲存並重建 shop-api". */
  saveLabel: string;
  /** Return a promise to let 儲存後離開 wait for the save; a rejected promise keeps the user on the page. */
  onSave: () => void | Promise<unknown>;
  onDiscard: () => void;
  /** Optional middle action, e.g. "只儲存". */
  secondary?: { label: string; onClick: () => void };
  /** True while the save request runs: every button is disabled and the primary reads 儲存中…. */
  saving?: boolean;
  /**
   * Ask before in-app navigation. Default true. React Router allows one blocker per page, so a page with
   * several save bars lets only one of them guard navigation.
   */
  guardNavigation?: boolean;
  /** Why the last save failed (raw reason, e.g. "磁碟唯讀（PLATFORM_DEGRADED）"); the primary becomes 再試一次. */
  error?: string;
}

function checkIsMac(): boolean {
  if (typeof navigator === "undefined") return false;
  const platform =
    (navigator as unknown as { userAgentData?: { platform?: string } }).userAgentData?.platform ||
    navigator.platform ||
    navigator.userAgent ||
    "";
  return /Mac|iPhone|iPod|iPad/i.test(platform);
}

export function SaveBar({
  changes,
  subject,
  effect,
  saveLabel,
  onSave,
  onDiscard,
  secondary,
  saving = false,
  guardNavigation = true,
  error,
}: SaveBarProps) {
  const { t } = useTranslation();
  const isMac = useMemo(() => checkIsMac(), []);
  const [leaving, setLeaving] = useState(false);
  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      guardNavigation &&
      changes > 0 &&
      (currentLocation.pathname !== nextLocation.pathname || currentLocation.search !== nextLocation.search),
  );

  useEffect(() => {
    if (changes <= 0) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === "s" || e.key === "S")) {
        e.preventDefault();
        // Holding the keys auto-repeats, and an IME may still be composing: save once, on a real press.
        if (!saving && !e.repeat && !e.isComposing) void onSave();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [changes, saving, onSave]);

  useEffect(() => {
    if (changes <= 0) return;
    const handleBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", handleBeforeUnload);
    return () => window.removeEventListener("beforeunload", handleBeforeUnload);
  }, [changes]);

  const saveAndLeave = async () => {
    setLeaving(true);
    try {
      await onSave();
      blocker.proceed?.();
    } catch {
      // The save failed; the bar shows the caller's error and the user stays on the page.
      blocker.reset?.();
    } finally {
      setLeaving(false);
    }
  };

  const shortcutText = isMac ? "⌘S" : "Ctrl S";
  const shortcutKeys = isMac ? "Meta+S" : "Control+S";
  const busy = saving || leaving;
  const leaveTitle = subject
    ? t("saveBar.leaveTitleOf", { subject, count: changes })
    : t("saveBar.leaveTitle", { count: changes });

  return (
    <>
      {changes > 0 && (
        <div className="hx-savebar" role="region" aria-label={t("saveBar.region")}>
          <span className="hx-savebar-info">
            <span className="hx-savebar-dot" aria-hidden="true" />
            <strong>
              {subject ? t("saveBar.changesOf", { subject, count: changes }) : t("saveBar.changes", { count: changes })}
            </strong>
            {error ? (
              <span className="hx-savebar-error"> · {t("saveBar.failed", { error })}</span>
            ) : (
              effect && <span className="hx-savebar-effect"> · {effect}</span>
            )}
          </span>

          <span className="hx-savebar-actions">
            <Button variant="secondary" onClick={onDiscard} disabled={busy}>
              {t("saveBar.discard")}
            </Button>
            {secondary && (
              <Button variant="secondary" onClick={secondary.onClick} disabled={busy}>
                {secondary.label}
              </Button>
            )}
            <Button
              variant="primary"
              icon={saving ? <ArrowClockwiseIcon aria-hidden="true" /> : <FloppyDiskIcon aria-hidden="true" />}
              disabled={busy}
              onClick={() => void onSave()}
              aria-keyshortcuts={shortcutKeys}
            >
              <span>{saving ? t("saveBar.saving") : error ? t("saveBar.retry") : saveLabel}</span>
              {!saving && <kbd className="hx-savebar-kbd">{shortcutText}</kbd>}
            </Button>
          </span>
        </div>
      )}

      {/* Confirmation without input: clicking the backdrop or pressing Esc means 留在這頁 (DESIGN.md dialog rules). */}
      <Dialog.Root
        open={blocker.state === "blocked"}
        onOpenChange={(open: boolean) => {
          if (!open && !leaving) blocker.reset?.();
        }}
      >
        <Dialog className="hx-leave" size="base">
          <Dialog.Title className="hx-leave-title">{leaveTitle}</Dialog.Title>
          <div className="hx-leave-actions">
            <Button variant="secondary" onClick={() => blocker.reset?.()} disabled={leaving}>
              {t("saveBar.stay")}
            </Button>
            <Button variant="secondary" onClick={() => blocker.proceed?.()} disabled={leaving}>
              {t("saveBar.leave")}
            </Button>
            <Button variant="primary" onClick={() => void saveAndLeave()} disabled={leaving}>
              {leaving ? t("saveBar.saving") : t("saveBar.saveAndLeave")}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </>
  );
}
