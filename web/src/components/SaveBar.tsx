import { Button } from "@cloudflare/kumo/components/button";
import { FloppyDiskIcon } from "@phosphor-icons/react";
import { useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import "./system.css";

/**
 * System-wide unsaved-changes bar (DESIGN.md「存檔一定看得到」). SLOT — props are fixed.
 */
export interface SaveBarProps {
  /** Number of unsaved changes. 0 renders nothing. */
  changes: number;
  /** What saving will cause, e.g. "存檔後要重建 shop-api 才會生效". Optional. */
  effect?: string;
  /** Primary button text that names the result, e.g. "儲存並重建 shop-api". */
  saveLabel: string;
  onSave: () => void;
  onDiscard: () => void;
  /** Optional middle action, e.g. "只儲存". */
  secondary?: { label: string; onClick: () => void };
  /** True while the save request runs: buttons disabled, primary shows Kumo loading. */
  saving?: boolean;
}

function checkIsMac(): boolean {
  if (typeof navigator === "undefined") {
    return false;
  }
  const platform =
    (navigator as unknown as { userAgentData?: { platform?: string } }).userAgentData?.platform ||
    navigator.platform ||
    navigator.userAgent ||
    "";
  return /Mac|iPhone|iPod|iPad/i.test(platform);
}

export function SaveBar({ changes, effect, saveLabel, onSave, onDiscard, secondary, saving = false }: SaveBarProps) {
  const { t } = useTranslation();
  const isMac = useMemo(() => checkIsMac(), []);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === "s" || e.key === "S")) {
        e.preventDefault();
        if (!saving) {
          onSave();
        }
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [saving, onSave]);

  useEffect(() => {
    const handleBeforeUnload = (e: BeforeUnloadEvent) => {
      if (changes > 0) {
        e.preventDefault();
        e.returnValue = "";
      }
    };

    window.addEventListener("beforeunload", handleBeforeUnload);
    return () => {
      window.removeEventListener("beforeunload", handleBeforeUnload);
    };
  }, [changes]);

  if (changes <= 0) {
    return null;
  }

  const shortcutText = isMac ? "⌘S" : "Ctrl S";
  const shortcutKeys = isMac ? "Meta+S" : "Control+S";

  return (
    <div className="hx-savebar" role="region" aria-label={t("saveBar.region")}>
      <span className="hx-savebar-info">
        <span className="hx-savebar-dot" aria-hidden="true" />
        <strong>{t("saveBar.changes", { count: changes })}</strong>
        {effect && <span className="hx-savebar-effect"> · {effect}</span>}
      </span>

      <span className="hx-savebar-actions">
        <Button variant="secondary" onClick={onDiscard} disabled={saving}>
          {t("saveBar.discard")}
        </Button>

        {secondary && (
          <Button variant="secondary" onClick={secondary.onClick} disabled={saving}>
            {secondary.label}
          </Button>
        )}

        <Button
          variant="primary"
          icon={<FloppyDiskIcon aria-hidden="true" />}
          loading={saving}
          disabled={saving}
          onClick={onSave}
          aria-keyshortcuts={shortcutKeys}
        >
          <span>{saveLabel}</span>
          <kbd className="hx-savebar-kbd">{shortcutText}</kbd>
        </Button>
      </span>
    </div>
  );
}
