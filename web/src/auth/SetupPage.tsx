import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, useNavigate } from "react-router";
import { useDocumentTitle } from "../shell/useDocumentTitle";
import { authApi, authKeys, isApiError } from "./api";
import { AUTH_PATHS } from "./nextPath";
import { AuthCard, AuthError, StepBar } from "./parts";
import { useRetryCountdown } from "./hooks";

/** Core's minimum password length (core/internal/iam/password.go MinPasswordLength). */
export const MIN_PASSWORD_LENGTH = 12;

const MESSAGES = { SETUP_CODE_INVALID: "auth.setup.codeInvalid" };

/**
 * First run (board Setup「建立擁有者帳號」): the one-time setup code printed by the installer, the owner's
 * user name and password. Core signs the new owner in, held to second-factor enrolment, which is next.
 */
export function SetupPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const client = useQueryClient();
  const status = useQuery({ queryKey: authKeys.setup, queryFn: authApi.setupStatus });
  const [setupCode, setSetupCode] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [touched, setTouched] = useState(false);
  const [created, setCreated] = useState(false);
  const retryIn = useRetryCountdown(error);
  useDocumentTitle(t("auth.setup.title"));

  if (status.data && !status.data.needsSetup && !created) return <Navigate to={AUTH_PATHS.login} replace />;
  if (status.isPending) return null;

  const shortPassword = password.length > 0 && password.length < MIN_PASSWORD_LENGTH;
  const ready = setupCode.trim() && username.trim() && password.length >= MIN_PASSWORD_LENGTH;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!ready || busy || retryIn > 0) return;
    setBusy(true);
    setError(null);
    try {
      await authApi.setup({ setupCode: setupCode.trim(), username: username.trim(), password });
      // This page leaves for enrolment itself; without the flag the cache update below would send it to login.
      setCreated(true);
      client.setQueryData(authKeys.setup, { needsSetup: false });
      // This page does not watch the session, so refetch it even while inactive: enrolment reads it next.
      await client.invalidateQueries({ queryKey: authKeys.session, refetchType: "all" });
      void navigate(`${AUTH_PATHS.enroll}?setup=1`, { replace: true });
    } catch (err) {
      if (isApiError(err, "SETUP_DONE")) {
        client.setQueryData(authKeys.setup, { needsSetup: false });
        return;
      }
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="hx-auth-stack">
      <AuthCard title={t("auth.setup.title")}>
        <StepBar
          steps={[
            { label: t("auth.steps.account"), state: "current" },
            { label: t("auth.steps.factor"), state: "todo" },
            { label: t("auth.steps.codes"), state: "todo" },
          ]}
        />
        <form className="hx-auth-form" onSubmit={(e) => void submit(e)} noValidate>
          <Input
            label={t("auth.setup.code")}
            autoComplete="off"
            spellCheck={false}
            autoFocus
            value={setupCode}
            error={touched && !setupCode.trim() ? t("auth.errors.required") : undefined}
            description={
              <>
                {t("auth.setup.codeHelp")} <code translate="no">sudo hazelminx setup-code</code>{" "}
                {t("auth.setup.codeHelpAfter")}
              </>
            }
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setSetupCode(e.target.value)}
          />
          <Input
            label={t("auth.username")}
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            value={username}
            error={touched && !username.trim() ? t("auth.errors.required") : undefined}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setUsername(e.target.value)}
          />
          <Input
            label={t("auth.password")}
            type="password"
            autoComplete="new-password"
            value={password}
            description={t("auth.setup.passwordRule", { count: MIN_PASSWORD_LENGTH })}
            error={
              (touched && !password) || shortPassword
                ? t("auth.setup.passwordShort", { count: MIN_PASSWORD_LENGTH })
                : undefined
            }
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setPassword(e.target.value)}
          />
          <AuthError error={error} messages={MESSAGES} retryIn={retryIn} />
          <Button variant="primary" type="submit" className="hx-auth-wide" disabled={busy || retryIn > 0}>
            {t("auth.continue")}
          </Button>
        </form>
      </AuthCard>
    </div>
  );
}
