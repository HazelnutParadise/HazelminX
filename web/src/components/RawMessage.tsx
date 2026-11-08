import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { ApiError } from "../api/client";
import { StatusPill, type StatusTone } from "./StatusPill";
import "./system.css";

/**
 * Third-party or system output kept verbatim next to a plain-language summary (UI-001, board ShellStates):
 * the summary is one status pill with the template sentence, the original sits in a collapsed <details>.
 */
export interface RawMessageProps {
  /** Plain-language summary in the UI language (fixed template filled with values). */
  summary: ReactNode;
  /** Original text exactly as received (any language, may be multi-line). Empty string hides the original block. */
  raw: string;
  /** Where the original came from, e.g. "zentinel --validate", "HazelminX Core". */
  source?: string;
  /** Extra identifier shown after the source, e.g. a trace ID. */
  meta?: string;
  /** Pill tone; failures are the common case. */
  tone?: StatusTone;
  /** Original block starts expanded. Default false. */
  defaultOpen?: boolean;
}

export function RawMessage({ summary, raw, source, meta, tone = "danger", defaultOpen = false }: RawMessageProps) {
  const { t } = useTranslation();
  const origin = [source, meta].filter(Boolean).join(" · ");
  return (
    <div className="hx-raw">
      <div>
        <StatusPill tone={tone}>{summary}</StatusPill>
      </div>
      {raw.trim().length > 0 && (
        <details className="hx-raw-details" open={defaultOpen}>
          <summary className="hx-raw-details-summary">
            {origin ? t("raw.source", { source: origin }) : t("raw.original")}
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
  const raw = error.raw.trim().length > 0 ? error.raw : error.message;
  const meta = error.traceId ? `trace ${error.traceId}` : undefined;
  return <RawMessage summary={summary} raw={raw} source="HazelminX Core" meta={meta} />;
}
