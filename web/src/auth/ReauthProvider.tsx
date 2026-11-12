import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { Input } from "@cloudflare/kumo/components/input";
import { LockSimpleIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useCallback, useId, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { authApi, authKeys, isApiError, type FactorMethod } from "./api";
import { AuthError, Notice, OtpInput } from "./parts";
import { useRetryCountdown } from "./hooks";
import { ReauthCancelled, ReauthContext, reauthMethods, type Ask } from "./reauth";
import { getPasskey, passkeyFailure } from "./webauthn";

/**
 * Hosts the reauthentication dialog (board MfaDialogs「…前 · 確認是你」). Credential changes need a sign-in
 * or reauthentication from the last 10 minutes; Core answers REAUTHENTICATION_REQUIRED otherwise.
 */
export function ReauthProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<{ title: string; resolve: () => void; reject: (e: unknown) => void } | null>(
    null,
  );
  const ask = useCallback<Ask>(
    (title) =>
      new Promise<void>((resolve, reject) =>
        setPending((prev) => {
          // One dialog at a time: an earlier request still waiting is cancelled, never left hanging.
          prev?.reject(new ReauthCancelled());
          return { title, resolve, reject };
        }),
      ),
    [],
  );
  return (
    <ReauthContext.Provider value={ask}>
      {children}
      {pending && (
        <ReauthDialog
          title={pending.title}
          onDone={() => {
            pending.resolve();
            setPending(null);
          }}
          onCancel={() => {
            pending.reject(new ReauthCancelled());
            setPending(null);
          }}
        />
      )}
    </ReauthContext.Provider>
  );
}

const MESSAGES = {
  INVALID_CREDENTIALS: "auth.errors.wrongPasswordOnly",
  SECOND_FACTOR_INVALID: "auth.errors.wrongCode",
  NOT_AVAILABLE: "auth.passkey.ipAddress",
};

function ReauthDialog({ title, onDone, onCancel }: { title: string; onDone: () => void; onCancel: () => void }) {
  const { t } = useTranslation();
  const factors = useQuery({ queryKey: authKeys.factors, queryFn: authApi.factors });
  const methods = reauthMethods(factors.data);
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [useRecovery, setUseRecovery] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [needPassword, setNeedPassword] = useState(false);
  const [cancelled, setCancelled] = useState(false);
  const retryIn = useRetryCountdown(error);
  const codeLabelId = useId();
  const submitted = useRef(false);

  const mode: FactorMethod | null = !methods.any ? null : useRecovery || !methods.totp ? "recovery-code" : "totp";
  // Without the factor list the dialog cannot tell which second factor to ask for.
  const blocked = retryIn > 0 || busy || factors.isPending || factors.isError;

  const run = async (proof?: { method: FactorMethod; code?: string; credential?: unknown }) => {
    if (!password) {
      setNeedPassword(true);
      return;
    }
    setBusy(true);
    setError(null);
    setCancelled(false);
    try {
      await authApi.reauthenticate(password, proof);
      submitted.current = true;
      onDone();
    } catch (err) {
      setError(err);
      setCode("");
    } finally {
      setBusy(false);
    }
  };

  const withPasskey = async () => {
    if (!password) {
      setNeedPassword(true);
      return;
    }
    setBusy(true);
    setError(null);
    setCancelled(false);
    try {
      const options = await authApi.reauthPasskeyOptions();
      const credential = await getPasskey(options);
      setBusy(false);
      await run({ method: "passkey", credential });
    } catch (err) {
      setBusy(false);
      if (passkeyFailure(err) === "cancelled") setCancelled(true);
      else setError(err);
    }
  };

  const submit = (e?: React.FormEvent) => {
    e?.preventDefault();
    if (blocked) return;
    if (mode === null) void run();
    else if (code) void run({ method: mode, code: mode === "recovery-code" ? code.trim() : code });
  };

  return (
    <Dialog.Root
      open
      disablePointerDismissal
      onOpenChange={(open: boolean) => {
        if (!open && !submitted.current) onCancel();
      }}
    >
      <Dialog className="hx-dialog" size="base">
        <form onSubmit={submit} noValidate>
          <Dialog.Title className="hx-dialog-title">{t("auth.reauth.title", { action: title })}</Dialog.Title>
          <div className="hx-dialog-body">
            <Input
              label={t("auth.password")}
              type="password"
              autoComplete="current-password"
              autoFocus
              value={password}
              error={needPassword && !password ? t("auth.errors.passwordRequired") : undefined}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setPassword(e.target.value)}
            />
            {methods.passkey && (
              <Button
                variant="secondary"
                className="hx-auth-wide"
                icon={<LockSimpleIcon aria-hidden="true" />}
                disabled={blocked}
                onClick={() => void withPasskey()}
              >
                {t("auth.usePasskey")}
              </Button>
            )}
            {mode === "totp" && (
              <div className="hx-field">
                <span className="hx-field-label" id={codeLabelId}>
                  {t("auth.reauth.totp")}
                </span>
                <OtpInput
                  value={code}
                  onChange={setCode}
                  labelledBy={codeLabelId}
                  disabled={retryIn > 0 || factors.isPending}
                  busy={busy}
                  invalid={isApiError(error, "SECOND_FACTOR_INVALID")}
                />
              </div>
            )}
            {mode === "recovery-code" && (
              <Input
                label={t("auth.recoveryCode")}
                autoComplete="off"
                spellCheck={false}
                value={code}
                disabled={blocked}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => setCode(e.target.value)}
              />
            )}
            {cancelled && <Notice tone="warn">{t("auth.passkey.cancelledRetry")}</Notice>}
            {factors.isError && (
              <div className="hx-auth-row">
                <AuthError error={factors.error} />
                <Button variant="secondary" onClick={() => void factors.refetch()}>
                  {t("auth.retry")}
                </Button>
              </div>
            )}
            <AuthError error={error} messages={MESSAGES} retryIn={retryIn} />
            {methods.totp && methods.recovery && (
              <button type="button" className="hx-link-button" onClick={() => setUseRecovery((v) => !v)}>
                {useRecovery ? t("auth.useAuthenticator") : t("auth.useRecoveryCode")}
              </button>
            )}
          </div>
          <div className="hx-dialog-actions">
            <Button variant="secondary" onClick={onCancel} disabled={busy}>
              {t("auth.cancel")}
            </Button>
            <Button variant="primary" type="submit" disabled={blocked || (mode !== null && !code)}>
              {t("auth.confirm")}
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  );
}
