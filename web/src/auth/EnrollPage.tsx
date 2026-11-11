import { Button } from "@cloudflare/kumo/components/button";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, useNavigate, useSearchParams } from "react-router";
import { PAGES } from "../app/navigation";
import { useDocumentTitle } from "../shell/useDocumentTitle";
import { authApi, authKeys, type EnrollmentResult, type SessionResponse } from "./api";
import { PasskeyButton, TotpSetup } from "./FactorSetup";
import { AUTH_PATHS, safeNext, withNext } from "./nextPath";
import { AuthCard, AuthError, Divider, Notice, RecoveryCodes, StepBar, type StepState } from "./parts";
import { useSession, useShellUser, useSignOut } from "./session";

type Phase = "choose" | "codes" | "done";

/**
 * Second-factor enrolment. After first-run setup (`?setup=1`, board Setup steps 2–4) and whenever an account
 * whose role requires a second factor signs in without one (board MfaDialogs「先設定二次驗證」): Core then
 * refuses every other page, so this screen stands on its own instead of opening over the shell.
 */
export function EnrollPage() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));
  const fromSetup = params.get("setup") === "1";
  const session = useSession();
  const [phase, setPhase] = useState<Phase>("choose");
  const [codes, setCodes] = useState<string[]>([]);
  useDocumentTitle(
    phase === "codes" ? t("auth.codes.title") : fromSetup ? t("auth.enroll.titleSetup") : t("auth.enroll.title"),
  );

  if (session.status === "loading") return null;
  if (session.status === "signed-out") return <Navigate to={withNext(AUTH_PATHS.login, next)} replace />;
  if (session.status === "error") {
    return (
      <div className="hx-auth-stack">
        <AuthCard title={t("auth.enroll.title")}>
          <AuthError error={session.error} />
        </AuthCard>
      </div>
    );
  }
  if (!session.session.secondFactorEnrollmentRequired && phase === "choose") return <Navigate to={next} replace />;

  return (
    <EnrollFlow
      session={session.session}
      fromSetup={fromSetup}
      next={next}
      phase={phase}
      codes={codes}
      onCodes={(c) => {
        setCodes(c);
        setPhase("codes");
      }}
      onDone={() => setPhase("done")}
    />
  );
}

function EnrollFlow({
  session,
  fromSetup,
  next,
  phase,
  codes,
  onCodes,
  onDone,
}: {
  session: SessionResponse;
  fromSetup: boolean;
  next: string;
  phase: Phase;
  codes: string[];
  onCodes: (codes: string[]) => void;
  onDone: () => void;
}) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const navigate = useNavigate();
  const signOut = useSignOut();
  const user = useShellUser(session);
  const [leaving, setLeaving] = useState(false);

  /** The account now has a second factor: refresh the session so the guard lets the user in. */
  const enter = async (to: string) => {
    setLeaving(true);
    await client.invalidateQueries({ queryKey: authKeys.session });
    await client.invalidateQueries({ queryKey: authKeys.factors });
    void navigate(to, { replace: true });
  };

  const onEnrolled = (r: EnrollmentResult) => {
    if (r.recoveryCodes?.length) onCodes(r.recoveryCodes);
    else if (fromSetup) onDone();
    else void enter(next);
  };

  const first: string = fromSetup ? t("auth.steps.account") : t("auth.steps.password");
  const steps = (factor: StepState, recovery: StepState) => [
    { label: first, state: "done" as const },
    { label: t("auth.steps.factor"), state: factor },
    { label: t("auth.steps.codes"), state: recovery },
  ];

  if (phase === "codes" && codes.length > 0) {
    return (
      <div className="hx-auth-stack">
        <AuthCard title={t("auth.codes.title")}>
          <StepBar steps={steps("done", "current")} />
          <RecoveryCodes
            codes={codes}
            account={session.user.username}
            continueLabel={t("auth.continue")}
            onContinue={() => (fromSetup ? onDone() : void enter(next))}
          />
        </AuthCard>
      </div>
    );
  }

  if (phase === "done") {
    const links = [
      { to: PAGES.settings.path, label: t("auth.done.domain") },
      { to: PAGES.projects.path, label: t("auth.done.github") },
      { to: PAGES.nodes.path, label: t("auth.done.hosts") },
    ];
    return (
      <div className="hx-auth-stack">
        <AuthCard title={t("auth.done.title")}>
          <p className="hx-auth-lead">{t("auth.done.lead")}</p>
          <ol className="hx-next-list">
            {links.map((l, i) => (
              <li key={l.to}>
                <button type="button" className="hx-next-item" disabled={leaving} onClick={() => void enter(l.to)}>
                  {i + 1} · {l.label}
                </button>
              </li>
            ))}
          </ol>
          <Button variant="primary" className="hx-auth-wide" disabled={leaving} onClick={() => void enter("/")}>
            {t("auth.done.overview")}
          </Button>
        </AuthCard>
      </div>
    );
  }

  return (
    <div className="hx-auth-stack">
      <AuthCard title={fromSetup ? t("auth.enroll.titleSetup") : t("auth.enroll.title")}>
        {!fromSetup && (
          <div className="hx-auth-pills">
            <Notice tone="warn">{t("auth.enroll.required", { role: user.role })}</Notice>
            <Notice tone="info">{t("auth.enroll.locked")}</Notice>
          </div>
        )}
        <StepBar steps={steps("current", "todo")} />
        <PasskeyButton
          onEnrolled={(r) => {
            // The authenticator app below opened an enrolment of its own; drop it, the passkey is enough.
            void authApi.removeTotp().catch(() => undefined);
            onEnrolled(r);
          }}
        />
        <Divider>{t("auth.enroll.orApp")}</Divider>
        <TotpSetup variant="card" submitLabel={t("auth.enroll.confirm")} onEnrolled={onEnrolled} />
        <div className="hx-auth-foot">
          <span className="hx-auth-muted">{t("auth.enroll.signedInAs", { name: user.name })}</span>
          <button type="button" className="hx-link-button" onClick={() => void signOut()}>
            {t("shell.signOut")}
          </button>
        </div>
      </AuthCard>
    </div>
  );
}
