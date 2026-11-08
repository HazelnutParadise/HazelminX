import { KumoLocaleProvider, LinkProvider, type KumoTranslationsPartial } from "@cloudflare/kumo/utils";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { createBrowserRouter, RouterProvider } from "react-router";
import { AppLink } from "./AppLink";
import { routes } from "./routes";
import { ShellStatusContext, type ShellStatus } from "./shellStatus";

function Providers({ children, status }: { children: React.ReactNode; status: ShellStatus }) {
  const { t } = useTranslation();
  const [queryClient] = useState(() => new QueryClient());
  return (
    <QueryClientProvider client={queryClient}>
      <KumoLocaleProvider translations={t("kumo", { returnObjects: true }) as KumoTranslationsPartial}>
        <LinkProvider component={AppLink}>
          <ShellStatusContext.Provider value={status}>{children}</ShellStatusContext.Provider>
        </LinkProvider>
      </KumoLocaleProvider>
    </QueryClientProvider>
  );
}

const router = createBrowserRouter(routes);

export function App({ status = {} }: { status?: ShellStatus }) {
  return (
    <Providers status={status}>
      <RouterProvider router={router} />
    </Providers>
  );
}
