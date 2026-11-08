import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { ApiError } from "../api/client";
import "./system.css";

/**
 * Third-party or system output kept verbatim next to a plain-language summary (UI-001).
 * SLOT — props are fixed.
 */
export interface RawMessageProps {
  /** Plain-language summary in the UI language (fixed template filled with values). */
  summary: ReactNode;
  /** Original text exactly as received (any language, may be multi-line). Empty string hides the original block. */
  raw: string;
  /** Where the original came from, e.g. "systemd", "Docker Engine", "HazelminX Core". */
  source?: string;
  /** Extra identifiers shown in mono after the source, e.g. a trace ID. */
  meta?: string;
  /** Original block starts expanded. Default false. */
  defaultOpen?: boolean;
}

export function RawMessage({ summary, raw, source, meta, defaultOpen = false }: RawMessageProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(defaultOpen);

  const hasRaw = raw.trim().length > 0;

  return (
    <div className="hx-raw">
      <div className="hx-raw-summary">{summary}</div>

      {hasRaw && (
        <details className="hx-raw-details" open={open} onToggle={(e) => setOpen(e.currentTarget.open)}>
          <summary className="hx-raw-details-summary">
            <span>{t("raw.original")}</span>
            {source && (
              <>
                <span> · </span>
                <span>{t("raw.source", { source })}</span>
              </>
            )}
            {meta && (
              <>
                <span> </span>
                <span className="hx-mono">{meta}</span>
              </>
            )}
          </summary>

          <pre translate="no" className="hx-raw-pre">
            {raw}
          </pre>
        </details>
      )}
    </div>
  );
}

/** RawMessage for a management API failure: summary from `errors.<code>` (fallback `errors.unknown`). */
export function ApiErrorMessage({ error }: { error: ApiError }) {
  const { t, i18n } = useTranslation();

  const summary = i18n.exists(`errors.${error.code}`)
    ? t(`errors.${error.code}`)
    : t("errors.unknown", { status: error.status });

  const raw = error.raw && error.raw.trim().length > 0 ? error.raw : error.message;
  const meta = error.traceId ? `trace ${error.traceId}` : undefined;

  return <RawMessage summary={summary} raw={raw} source="HazelminX Core" meta={meta} />;
}
