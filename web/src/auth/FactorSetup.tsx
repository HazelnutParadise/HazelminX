import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { CopyIcon, LockSimpleIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { activeLocale } from "../i18n";
import { browserTimeZone, formatDateTime, parseRfc3339, toRfc3339Utc } from "../lib/time";
import { useNow } from "../shell/useNow";
import { authApi, isApiError, type EnrollmentResult } from "./api";
import { AuthError, Notice, OtpInput, QrCode } from "./parts";
import { groupSecret, useCopy, useRetryCountdown } from "./hooks";
import { ReauthCancelled, useReauth } from "./reauth";
import { createPasskey, defaultPasskeyName, passkeyBlock, passkeyFailure, type PasskeyBlock } from "./webauthn";

const TOTP_MESSAGES = { SECOND_FACTOR_INVALID: "auth.totp.wrongCode" };

/**
 * Authenticator-app enrolment (board Setup「綁定二次驗證」, MfaDialogs「新增驗證器」): QR code, the key for
 * manual entry, the server's clock and the first code. Starting it replaces any unconfirmed enrolment.
 * `variant` only changes the layout: inline in the setup card or inside the dialog.
 */
export function TotpSetup({
  variant,
  onEnrolled,
  onCancel,
  submitLabel,
}: {
  variant: "card" | "dialog";
  onEnrolled: (result: EnrollmentResult) => void;
  onCancel?: () => void;
  submitLabel: string;
}) {
  const { t } = useTranslation();
  const reauth = useReauth();
  // One enrolment per mount: the query dedupes StrictMode's double effect, so Core never holds a newer
  // pending secret than the one on screen.
  const [attempt] = useState(() => Math.random().toString(36).slice(2));
  const begun = useQuery({
    queryKey: ["me", "totp-enrollment", attempt],
    queryFn: async () => {
      const e = await reauth(() => authApi.beginTotp(), t("auth.totp.reauthAction"));
      const server = parseRfc3339(e.serverTime);
      return { ...e, clockOffsetMs: server ? server.getTime() - Date.now() : 0 };
    },
    retry: false,
    staleTime: Infinity,
    gcTime: 0,
    refetchOnWindowFocus: false,
  });
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [copied, copy] = useCopy();
  const retryIn = useRetryCountdown(error);
  const now = useNow(1000);
  const codeLabelId = useId();
  const enrolled = useRef(false);

  const e = begun.data;
  const serverNow = e ? new Date(now.getTime() + e.clockOffsetMs) : null;
  const expiresAt = e ? parseRfc3339(e.expiresAt) : null;
  const expired = (!!serverNow && !!expiresAt && serverNow >= expiresAt) || isApiError(error, "ALREADY_EXISTS");

  const restart = () => {
    setError(null);
    setCode("");
    void begun.refetch();
  };

  const confirm = async (value = code) => {
    if (value.length !== 6 || busy || retryIn > 0 || expired) return;
    setBusy(true);
    setError(null);
    try {
      const result = await authApi.confirmTotp(value);
      enrolled.current = true;
      onEnrolled(result);
    } catch (err) {
      setError(err);
      setCode("");
    } finally {
      setBusy(false);
    }
  };

  const cancel = () => {
    // Drop the pending secret; Core keeps a confirmed authenticator untouched.
    if (e && !enrolled.current) void authApi.removeTotp().catch(() => undefined);
    onCancel?.();
  };

  if (begun.isPending) return <p className="hx-auth-muted">{t("loading")}</p>;
  if (begun.isError) {
    return (
      <div className="hx-factor">
        {begun.error instanceof ReauthCancelled ? (
          <Notice tone="warn">{t("auth.reauth.cancelled")}</Notice>
        ) : (
          <AuthError error={begun.error} />
        )}
        <div className="hx-auth-row">
          {onCancel && (
            <Button variant="secondary" onClick={onCancel}>
              {t("auth.cancel")}
            </Button>
          )}
          <Button variant="secondary" onClick={restart}>
            {t("auth.retry")}
          </Button>
        </div>
      </div>
    );
  }

  const secret = groupSecret(e!.secret);
  const serverTime = serverNow
    ? formatDateTime(toRfc3339Utc(serverNow), activeLocale(), browserTimeZone(), "time")
    : "";
  const keyBlock = (
    <>
      <code className="hx-secret" translate="no">
        {secret}
      </code>
      <Button variant="secondary" icon={<CopyIcon aria-hidden="true" />} onClick={() => void copy(e!.secret)}>
        {copied ? t("auth.copied") : t("auth.totp.copyKey")}
      </Button>
    </>
  );

  return (
    <form
      className={`hx-factor hx-totp hx-totp--${variant}`}
      onSubmit={(ev) => {
        ev.preventDefault();
        void confirm();
      }}
      noValidate
    >
      <div className="hx-totp-scan">
        <QrCode value={e!.uri} label={t("auth.totp.qrLabel")} />
        {variant === "dialog" ? (
          <div className="hx-totp-key">
            <span className="hx-auth-muted">{t("auth.totp.manual")}</span>
            {keyBlock}
            <span className="hx-auth-muted">{t("auth.totp.serverTime", { time: serverTime })}</span>
          </div>
        ) : (
          <details className="hx-totp-details">
            <summary>{t("auth.totp.showKey")}</summary>
            <div className="hx-totp-key">
              {keyBlock}
              <span className="hx-auth-muted">{t("auth.totp.serverTime", { time: serverTime })}</span>
            </div>
          </details>
        )}
      </div>
      <div className="hx-field">
        <span className="hx-field-label" id={codeLabelId}>
          {variant === "card" ? t("auth.totp.enterCodeApp") : t("auth.totp.enterCode")}
        </span>
        <OtpInput
          value={code}
          onChange={setCode}
          onComplete={(v) => void confirm(v)}
          labelledBy={codeLabelId}
          disabled={retryIn > 0 || expired}
          busy={busy}
          invalid={isApiError(error, "SECOND_FACTOR_INVALID")}
        />
      </div>
      {expired ? (
        <div className="hx-auth-row">
          <Notice tone="warn">{t("auth.totp.expired")}</Notice>
          <Button variant="secondary" onClick={restart}>
            {t("auth.totp.restart")}
          </Button>
        </div>
      ) : (
        <AuthError error={error} messages={TOTP_MESSAGES} retryIn={retryIn} />
      )}
      <div className={variant === "dialog" ? "hx-dialog-actions" : undefined}>
        {onCancel && (
          <Button variant="secondary" onClick={cancel} disabled={busy}>
            {t("auth.cancel")}
          </Button>
        )}
        <Button
          variant="primary"
          type="submit"
          className={variant === "card" ? "hx-auth-wide" : undefined}
          disabled={busy || code.length !== 6 || retryIn > 0 || expired}
        >
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}

type PasskeyState =
  | { kind: "idle" }
  | { kind: "cancelled" }
  | { kind: "blocked"; why: PasskeyBlock }
  | { kind: "error"; error: unknown };

const PASSKEY_MESSAGES = {
  ALREADY_EXISTS: "auth.passkey.alreadyRegistered",
  SECOND_FACTOR_INVALID: "auth.passkey.rejected",
};

/** Runs the passkey registration ceremony; `name` labels it in the account's list. */
function usePasskeyRegistration(onEnrolled: (r: EnrollmentResult) => void) {
  const { t } = useTranslation();
  const reauth = useReauth();
  const [busy, setBusy] = useState(false);
  const [state, setState] = useState<PasskeyState>(() => {
    const why = passkeyBlock();
    return why ? { kind: "blocked", why } : { kind: "idle" };
  });
  const start = async (name: string) => {
    setBusy(true);
    setState({ kind: "idle" });
    try {
      const options = await reauth(() => authApi.passkeyOptions(), t("auth.passkey.reauthAction"));
      const credential = await createPasskey(options);
      const result = await authApi.registerPasskey(name.trim() || defaultPasskeyName(), credential);
      onEnrolled(result);
    } catch (err) {
      if (err instanceof ReauthCancelled) setState({ kind: "idle" });
      else if (isApiError(err, "NOT_AVAILABLE")) setState({ kind: "blocked", why: "ip-address" });
      else if (passkeyFailure(err) === "cancelled") setState({ kind: "cancelled" });
      else if (passkeyFailure(err) === "unsupported") setState({ kind: "blocked", why: "unsupported" });
      else setState({ kind: "error", error: err });
    } finally {
      setBusy(false);
    }
  };
  return { busy, state, start };
}

/** Status rows of the passkey dialog (board MfaDialogs「新增 Passkey」). */
function PasskeyStatus({ state }: { state: PasskeyState }) {
  const { t } = useTranslation();
  const { hostname, protocol } = window.location;
  if (state.kind === "blocked") {
    return state.why === "ip-address" ? (
      <div className="hx-check hx-check--danger" role="alert">
        <span className="hx-check-shape" aria-hidden="true">
          ■
        </span>
        <div>
          <strong>{t("auth.passkey.ipAddress")}</strong>
          <span className="hx-mono">{t("auth.passkey.ipAddressDetail", { host: hostname })}</span>
        </div>
        <span className="hx-check-note">{t("auth.passkey.useDomain")}</span>
      </div>
    ) : (
      <div className="hx-check hx-check--danger" role="alert">
        <span className="hx-check-shape" aria-hidden="true">
          ■
        </span>
        <div>
          <strong>{t("auth.passkey.unsupported")}</strong>
        </div>
      </div>
    );
  }
  if (state.kind === "cancelled") {
    return (
      <div className="hx-check hx-check--warn" role="status">
        <span className="hx-check-shape" aria-hidden="true">
          ○
        </span>
        <div>
          <strong>{t("auth.passkey.cancelled")}</strong>
          <span>{t("auth.passkey.cancelledDetail")}</span>
        </div>
        <span className="hx-check-note">{t("auth.passkey.tryAgain")}</span>
      </div>
    );
  }
  return (
    <>
      <div className="hx-check hx-check--ok">
        <span className="hx-check-shape" aria-hidden="true">
          ●
        </span>
        <div>
          <strong>{t("auth.passkey.available")}</strong>
          <span className="hx-mono">
            {hostname} · {protocol === "https:" ? "HTTPS" : "HTTP"}
          </span>
        </div>
      </div>
      {state.kind === "error" && <AuthError error={state.error} messages={PASSKEY_MESSAGES} />}
    </>
  );
}

/** The setup card's 「用這台裝置建立 Passkey」 button with its outcome under it. */
export function PasskeyButton({ onEnrolled }: { onEnrolled: (r: EnrollmentResult) => void }) {
  const { t } = useTranslation();
  const { busy, state, start } = usePasskeyRegistration(onEnrolled);
  return (
    <div className="hx-factor">
      <Button
        variant="primary"
        className="hx-auth-wide"
        icon={<LockSimpleIcon aria-hidden="true" />}
        disabled={busy || state.kind === "blocked"}
        onClick={() => void start(defaultPasskeyName())}
      >
        {t("auth.passkey.createHere")}
      </Button>
      {state.kind !== "idle" && <PasskeyStatus state={state} />}
    </div>
  );
}

/** Body of the 「新增 Passkey」 dialog: device name, whether this address can use passkeys, start. */
export function PasskeyForm({
  onEnrolled,
  onCancel,
}: {
  onEnrolled: (r: EnrollmentResult) => void;
  onCancel: () => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(() => defaultPasskeyName());
  const { busy, state, start } = usePasskeyRegistration(onEnrolled);
  return (
    <form
      className="hx-factor"
      onSubmit={(e) => {
        e.preventDefault();
        void start(name);
      }}
      noValidate
    >
      <Input
        label={t("auth.passkey.deviceName")}
        value={name}
        maxLength={64}
        onChange={(e: React.ChangeEvent<HTMLInputElement>) => setName(e.target.value)}
      />
      <PasskeyStatus state={state} />
      <div className="hx-dialog-actions">
        <Button variant="secondary" onClick={onCancel} disabled={busy}>
          {t("auth.cancel")}
        </Button>
        <Button variant="primary" type="submit" disabled={busy || state.kind === "blocked"}>
          {t("auth.passkey.start")}
        </Button>
      </div>
    </form>
  );
}
