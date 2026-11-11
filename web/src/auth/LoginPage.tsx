import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LockSimpleIcon } from "@phosphor-icons/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, useSearchParams } from "react-router";
import { parseRfc3339 } from "../lib/time";
import { useDocumentTitle } from "../shell/useDocumentTitle";
import { useNow } from "../shell/useNow";
import { authApi, authKeys, isApiError, type FactorMethod, type FactorProof } from "./api";
import { AUTH_PATHS, safeNext, withNext } from "./nextPath";
import { AuthCard, AuthError, Divider, Notice, OtpInput } from "./parts";
import { useRetryCountdown } from "./hooks";
import { useSession } from "./session";
import { getPasskey, passkeyBlock, passkeyFailure } from "./webauthn";

interface Challenge {
  token: string;
  methods: string[];
  expiresAt?: string;
  username: string;
}

/**
 * Sign-in (board Login): password, then the second step when the account has a second factor. Success
 * refreshes the session; the redirect below then opens `next`, or enrolment when the account must add a
 * second factor first.
 */
export function LoginPage() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));
  const session = useSession();
  const setup = useQuery({ queryKey: authKeys.setup, queryFn: authApi.setupStatus });
  const client = useQueryClient();
  const [challenge, setChallenge] = useState<Challenge | null>(null);
  const [username, setUsername] = useState("");
  const [expired, setExpired] = useState(false);
  useDocumentTitle(t("auth.login.title"));

  if (session.status === "signed-in") {
    const s = session.session;
    return <Navigate to={s.secondFactorEnrollmentRequired ? withNext(AUTH_PATHS.enroll, next) : next} replace />;
  }
  if (setup.data?.needsSetup) return <Navigate to={AUTH_PATHS.setup} replace />;
  if (session.status === "loading") return null;

  const signedIn = () => client.invalidateQueries({ queryKey: authKeys.session });

  return (
    <div className="hx-auth-stack">
      {challenge ? (
        <SecondFactorStep
          challenge={challenge}
          onBack={(timedOut) => {
            setChallenge(null);
            setExpired(timedOut);
          }}
          onSignedIn={signedIn}
        />
      ) : (
        <PasswordStep
          username={username}
          onUsername={setUsername}
          expired={expired}
          onChallenge={(c) => {
            setExpired(false);
            setChallenge(c);
          }}
          onSignedIn={signedIn}
        />
      )}
    </div>
  );
}

const PASSWORD_MESSAGES = { INVALID_CREDENTIALS: "auth.errors.wrongPassword" };

function PasswordStep({
  username,
  onUsername,
  expired,
  onChallenge,
  onSignedIn,
}: {
  username: string;
  onUsername: (v: string) => void;
  expired: boolean;
  onChallenge: (c: Challenge) => void;
  onSignedIn: () => Promise<unknown>;
}) {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const retryIn = useRetryCountdown(error);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (busy || retryIn > 0 || !username.trim() || !password) return;
    setBusy(true);
    setError(null);
    try {
      const res = await authApi.login({ username: username.trim(), password });
      if (res.step === "second-factor" && res.challengeToken) {
        onChallenge({
          token: res.challengeToken,
          methods: res.methods ?? [],
          expiresAt: res.challengeExpiresAt,
          username: username.trim(),
        });
        return;
      }
      await onSignedIn();
    } catch (err) {
      setError(err);
      setPassword("");
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthCard title={t("auth.login.title")} subtitle={<span className="hx-mono">{window.location.host}</span>}>
      <form className="hx-auth-form" onSubmit={(e) => void submit(e)} noValidate>
        {expired && <Notice tone="warn">{t("auth.login.challengeExpired")}</Notice>}
        <Input
          label={t("auth.username")}
          autoComplete="username"
          autoCapitalize="none"
          spellCheck={false}
          autoFocus
          value={username}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => onUsername(e.target.value)}
        />
        <Input
          label={t("auth.password")}
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setPassword(e.target.value)}
        />
        <AuthError error={error} messages={PASSWORD_MESSAGES} retryIn={retryIn} />
        <Button
          variant="primary"
          type="submit"
          className="hx-auth-wide"
          disabled={busy || retryIn > 0 || !username.trim() || !password}
        >
          {t("auth.continue")}
        </Button>
      </form>
      <details className="hx-auth-help">
        <summary>{t("auth.login.cannotSignIn")}</summary>
        <p>{t("auth.login.lostFactor")}</p>
        <p>
          {t("auth.login.lostPassword")} <code translate="no">sudo hazelminx recover-login</code>
        </p>
      </details>
    </AuthCard>
  );
}

const FACTOR_MESSAGES = {
  SECOND_FACTOR_INVALID: "auth.errors.wrongCode",
  NOT_AVAILABLE: "auth.passkey.ipAddress",
};

function SecondFactorStep({
  challenge,
  onBack,
  onSignedIn,
}: {
  challenge: Challenge;
  onBack: (timedOut: boolean) => void;
  onSignedIn: () => Promise<unknown>;
}) {
  const { t } = useTranslation();
  const has = (m: FactorMethod) => challenge.methods.includes(m);
  const passkey = has("passkey") && passkeyBlock() === null;
  const [mode, setMode] = useState<"code" | "recovery" | "none">(() =>
    has("totp") ? "code" : passkey ? "none" : "recovery",
  );
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [cancelled, setCancelled] = useState(false);
  const retryIn = useRetryCountdown(error);
  const now = useNow(1000);
  const codeLabelId = useId();
  const blocked = busy || retryIn > 0;

  const expiresAt = challenge.expiresAt ? parseRfc3339(challenge.expiresAt) : null;
  const timedOut = !!expiresAt && now >= expiresAt;
  useEffect(() => {
    if (timedOut) onBack(true);
  }, [timedOut, onBack]);

  const send = async (proof: FactorProof) => {
    setBusy(true);
    setError(null);
    setCancelled(false);
    try {
      await authApi.secondFactor(challenge.token, proof);
      await onSignedIn();
    } catch (err) {
      if (isApiError(err, "LOGIN_CHALLENGE_INVALID")) {
        onBack(true);
        return;
      }
      setError(err);
      setCode("");
    } finally {
      setBusy(false);
    }
  };

  const withPasskey = async () => {
    setBusy(true);
    setError(null);
    setCancelled(false);
    try {
      const options = await authApi.loginPasskeyOptions(challenge.token);
      const credential = await getPasskey(options);
      setBusy(false);
      await send({ method: "passkey", credential });
    } catch (err) {
      setBusy(false);
      if (isApiError(err, "LOGIN_CHALLENGE_INVALID")) onBack(true);
      else if (passkeyFailure(err) === "cancelled") setCancelled(true);
      else setError(err);
    }
  };

  const canToggle = has("recovery-code") && (has("totp") || passkey);
  return (
    <AuthCard title={t("auth.second.title")} subtitle={t("auth.second.subtitle", { username: challenge.username })}>
      <form
        className="hx-auth-form"
        onSubmit={(e) => {
          e.preventDefault();
          if (!blocked && code)
            void send({ method: mode === "recovery" ? "recovery-code" : "totp", code: code.trim() });
        }}
        noValidate
      >
        {passkey && (
          <Button
            variant="primary"
            className="hx-auth-wide"
            icon={<LockSimpleIcon aria-hidden="true" />}
            disabled={blocked}
            onClick={() => void withPasskey()}
          >
            {t("auth.usePasskey")}
          </Button>
        )}
        {mode === "code" && (
          <>
            {passkey ? (
              <Divider id={codeLabelId}>{t("auth.second.orCode")}</Divider>
            ) : (
              <span className="hx-field-label" id={codeLabelId}>
                {t("auth.second.code")}
              </span>
            )}
            <OtpInput
              value={code}
              onChange={setCode}
              onComplete={(v) => void send({ method: "totp", code: v })}
              labelledBy={codeLabelId}
              disabled={retryIn > 0}
              busy={busy}
              invalid={isApiError(error, "SECOND_FACTOR_INVALID")}
              autoFocus={!passkey}
            />
          </>
        )}
        {mode === "recovery" && (
          <>
            <Input
              label={t("auth.recoveryCode")}
              autoComplete="off"
              autoCapitalize="characters"
              spellCheck={false}
              autoFocus
              value={code}
              disabled={blocked}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setCode(e.target.value)}
            />
            <Button
              variant={passkey ? "secondary" : "primary"}
              type="submit"
              className="hx-auth-wide"
              disabled={blocked || !code.trim()}
            >
              {t("auth.confirm")}
            </Button>
          </>
        )}
        {cancelled && <Notice tone="warn">{t("auth.passkey.cancelledRetry")}</Notice>}
        <AuthError error={error} messages={FACTOR_MESSAGES} retryIn={retryIn} />
        <div className="hx-auth-row hx-auth-row--split">
          {canToggle ? (
            <Button
              variant="secondary"

              onClick={() => {
                setCode("");
                setError(null);
                setMode(mode === "recovery" ? (has("totp") ? "code" : "none") : "recovery");
              }}
            >
              {mode === "recovery"
                ? has("totp")
                  ? t("auth.useAuthenticator")
                  : t("auth.usePasskey")
                : t("auth.useRecoveryCode")}
            </Button>
          ) : (
            <span />
          )}
          <Button variant="secondary" onClick={() => onBack(false)}>
            {t("auth.back")}
          </Button>
        </div>
      </form>
    </AuthCard>
  );
}
