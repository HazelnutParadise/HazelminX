import { KumoLocaleProvider, LinkProvider, type KumoTranslationsPartial } from "@cloudflare/kumo/utils";
import { QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { createBrowserRouter, RouterProvider } from "react-router";
import { ReauthProvider } from "../auth/ReauthProvider";
import { createQueryClient } from "../auth/session";
import { AppLink } from "./AppLink";
import { routes } from "./routes";
import { ShellStatusContext, type ShellStatus } from "./shellStatus";

/** Everything around the router: queries, Kumo locale and links, shell status, reauthentication. */
export function AppProviders({ children, status }: { children: React.ReactNode; status: ShellStatus }) {
  const { t } = useTranslation();
  const [queryClient] = useState(createQueryClient);
  return (
    <QueryClientProvider client={queryClient}>
      <KumoLocaleProvider translations={t("kumo", { returnObjects: true }) as KumoTranslationsPartial}>
        <LinkProvider component={AppLink}>
          <ShellStatusContext.Provider value={status}>
            <ReauthProvider>{children}</ReauthProvider>
          </ShellStatusContext.Provider>
        </LinkProvider>
      </KumoLocaleProvider>
    </QueryClientProvider>
  );
}

const router = createBrowserRouter(routes);

export function App({ status = {} }: { status?: ShellStatus }) {
  return (
    <AppProviders status={status}>
      <RouterProvider router={router} />
    </AppProviders>
  );
}
