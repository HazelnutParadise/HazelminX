import { useTranslation } from "react-i18next";
import { Outlet } from "react-router";
import { ShellStatusContext } from "../app/shellStatus";
import { TopBar } from "../shell/TopBar";
import "../shell/shell.css";
import "./auth.css";

const SIGNED_OUT = { signedOut: true };

/** Login, setup and enrolment: the signed-out top bar (brand, theme, language) over one centred column. */
export function AuthLayout() {
  const { t } = useTranslation();
  return (
    <ShellStatusContext.Provider value={SIGNED_OUT}>
      <div className="hx-shell">
        <a className="hx-skip" href="#main">
          {t("shell.skipToContent")}
        </a>
        <TopBar />
        <main id="main" className="hx-auth-main" tabIndex={-1}>
          <Outlet />
        </main>
      </div>
    </ShellStatusContext.Provider>
  );
}
