import { Button } from "@cloudflare/kumo/components/button";
import { Checkbox } from "@cloudflare/kumo/components/checkbox";
import { CopyIcon, DownloadSimpleIcon } from "@phosphor-icons/react";
import { useId, useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { encode } from "uqr";
import type { ApiError } from "../api/client";
import { ApiErrorMessage } from "../components/RawMessage";
import { StatusPill, type StatusTone } from "../components/StatusPill";
import { isApiError } from "./api";
import { formatWait, OTP_LENGTH, useCopy } from "./hooks";
import "./auth.css";

/** Card used by the login, setup and enrolment screens (boards Login, Setup): brand row, title, body. */
export function AuthCard({ title, subtitle, children }: { title: string; subtitle?: ReactNode; children: ReactNode }) {
  const { t } = useTranslation();
  return (
    <section className="hx-auth-card" aria-labelledby="hx-auth-title">
      <div className="hx-auth-brand">
        <span className="hx-logo" aria-hidden="true" />
        <span>{t("app.name")}</span>
      </div>
      <h1 id="hx-auth-title" className="hx-auth-title">
        {title}
      </h1>
      {subtitle && <div className="hx-auth-sub">{subtitle}</div>}
      {children}
    </section>
  );
}

export type StepState = "done" | "current" | "todo";

/** Three-segment progress (board Setup): done green, current blue, the rest faint. */
export function StepBar({ steps }: { steps: { label: string; state: StepState }[] }) {
  const { t } = useTranslation();
  return (
    <ol className="hx-steps" aria-label={t("auth.progress")}>
      {steps.map((s) => (
        <li
          key={s.label}
          className={`hx-step hx-step--${s.state}`}
          aria-current={s.state === "current" ? "step" : undefined}
        >
          <span className="hx-step-bar" aria-hidden="true" />
          <span className="hx-step-label">
            {s.label}
            {s.state === "done" && <span className="hx-visually-hidden">{t("auth.stepDone")}</span>}
          </span>
        </li>
      ))}
    </ol>
  );
}

/** Divider with a word in the middle (「或」, 「或輸入驗證器上的 6 位數」). */
export function Divider({ children, id }: { children: ReactNode; id?: string }) {
  return (
    <div className="hx-divider" id={id}>
      <span>{children}</span>
    </div>
  );
}

/**
 * Six-digit code entry drawn as six boxes (board Login). One real input underneath, so paste, one-time-code
 * autofill and screen readers work as for any text field; the boxes only mirror its value.
 */
export function OtpInput({
  value,
  onChange,
  onComplete,
  label,
  labelledBy,
  disabled = false,
  busy = false,
  invalid = false,
  autoFocus = false,
}: {
  value: string;
  onChange: (value: string) => void;
  onComplete?: (value: string) => void;
  label?: string;
  labelledBy?: string;
  disabled?: boolean;
  /** A submitted code is being checked: the field stays focused but takes no input. */
  busy?: boolean;
  invalid?: boolean;
  autoFocus?: boolean;
}) {
  const [focused, setFocused] = useState(false);
  return (
    <div className={`hx-otp${disabled ? " hx-otp--disabled" : ""}${invalid ? " hx-otp--invalid" : ""}`}>
      <input
        className="hx-otp-input"
        type="text"
        inputMode="numeric"
        autoComplete="one-time-code"
        pattern="[0-9]*"
        maxLength={OTP_LENGTH}
        aria-label={label}
        aria-labelledby={labelledBy}
        aria-invalid={invalid || undefined}
        disabled={disabled}
        readOnly={busy}
        aria-busy={busy || undefined}
        autoFocus={autoFocus}
        value={value}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        onChange={(e) => {
          const digits = e.target.value.replace(/\D/g, "").slice(0, OTP_LENGTH);
          onChange(digits);
          if (digits.length === OTP_LENGTH && digits !== value) onComplete?.(digits);
        }}
      />
      <div className="hx-otp-boxes" aria-hidden="true">
        {Array.from({ length: OTP_LENGTH }, (_, i) => (
          <span
            key={i}
            className={
              focused && i === Math.min(value.length, OTP_LENGTH - 1) ? "hx-otp-box hx-otp-box--active" : "hx-otp-box"
            }
          >
            {value[i] ?? ""}
          </span>
        ))}
      </div>
    </div>
  );
}

/**
 * Error line under an auth form. Codes these screens expect get their own sentence; anything else is shown
 * with the original response (UI-001). `messages` maps a code to the i18n key used on this form.
 */
export function AuthError({
  error,
  messages = {},
  retryIn,
}: {
  error: unknown;
  messages?: Record<string, string>;
  /** Seconds left of a throttle wait (useRetryCountdown); without it the wait Core reported is shown. */
  retryIn?: number;
}) {
  const { t } = useTranslation();
  if (!error) return null;
  if (isApiError(error, "TOO_MANY_ATTEMPTS")) {
    const wait = retryIn ?? error.retryAfterSeconds ?? 60;
    if (wait <= 0) return null;
    // The alert is read once with the wait Core reported; the visible countdown is not announced every second.
    return (
      <div className="hx-notice">
        <span className="hx-visually-hidden" role="alert">
          {t("auth.errors.tooManyAttemptsSr", { wait: formatWait(error.retryAfterSeconds ?? 60) })}
        </span>
        <span aria-hidden="true">
          <StatusPill tone="danger">{t("auth.errors.tooManyAttempts", { wait: formatWait(wait) })}</StatusPill>
        </span>
      </div>
    );
  }
  if (isApiError(error)) {
    const key = messages[error.code];
    if (key) return <Notice tone="danger">{t(key)}</Notice>;
    return (
      <div role="alert">
        <ApiErrorMessage error={error as ApiError} />
      </div>
    );
  }
  return <Notice tone="danger">{error instanceof Error ? error.message : JSON.stringify(error)}</Notice>;
}

/** A status pill announced to screen readers when it appears. */
export function Notice({ tone, children }: { tone: StatusTone; children: ReactNode }) {
  return (
    <div className="hx-notice" role={tone === "danger" ? "alert" : "status"}>
      <StatusPill tone={tone}>{children}</StatusPill>
    </div>
  );
}

/** QR code of an otpauth:// URI, drawn on white with a quiet zone so phones read it in dark mode too. */
export function QrCode({ value, label }: { value: string; label: string }) {
  const { path, size } = useMemo(() => {
    const qr = encode(value, { border: 4 });
    let d = "";
    qr.data.forEach((row, y) =>
      row.forEach((on, x) => {
        if (on) d += `M${x} ${y}h1v1h-1z`;
      }),
    );
    return { path: d, size: qr.size };
  }, [value]);
  return (
    <svg className="hx-qr" viewBox={`0 0 ${size} ${size}`} role="img" aria-label={label} shapeRendering="crispEdges">
      <rect width={size} height={size} fill="#fff" />
      <path d={path} fill="#000" />
    </svg>
  );
}

/**
 * Recovery codes shown once (board Setup「存好復原碼」): the codes, download, copy and an acknowledgement
 * that unlocks the continue button.
 */
export function RecoveryCodes({
  codes,
  account,
  continueLabel,
  onContinue,
}: {
  codes: string[];
  account: string;
  continueLabel: string;
  onContinue: () => void;
}) {
  const { t } = useTranslation();
  const [saved, setSaved] = useState(false);
  const [copied, copy] = useCopy();
  const listId = useId();
  const text = codes.join("\n");
  const download = () => {
    const body = `${t("auth.codes.fileHeader", { account, host: window.location.host })}\n\n${text}\n`;
    const url = URL.createObjectURL(new Blob([body], { type: "text/plain;charset=utf-8" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "hazelminx-recovery-codes.txt";
    a.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 0);
  };
  return (
    <div className="hx-codes">
      <p className="hx-auth-lead" id={listId}>
        {t("auth.codes.lead")}
      </p>
      <ul className="hx-codes-list" aria-labelledby={listId} translate="no">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      <div className="hx-auth-row">
        <Button variant="secondary" icon={<DownloadSimpleIcon aria-hidden="true" />} onClick={download}>
          {t("auth.codes.download")}
        </Button>
        <Button variant="secondary" icon={<CopyIcon aria-hidden="true" />} onClick={() => void copy(text)}>
          {copied ? t("auth.copied") : t("auth.copy")}
        </Button>
      </div>
      <Checkbox label={t("auth.codes.saved")} checked={saved} onCheckedChange={(v: boolean) => setSaved(v)} />
      <Button variant="primary" className="hx-auth-wide" disabled={!saved} onClick={onContinue}>
        {continueLabel}
      </Button>
    </div>
  );
}
