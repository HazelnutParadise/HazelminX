import {
  KumoLocaleProvider,
  LinkProvider,
  type KumoTranslationsPartial,
  type LinkComponentProps,
} from "@cloudflare/kumo/utils";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { forwardRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { createBrowserRouter, Link as RouterLink, RouterProvider } from "react-router";
import { routes } from "./routes";
import { ShellStatusContext, type ShellStatus } from "./shellStatus";

/** Kumo renders plain anchors; route them through React Router so navigation stays client-side. */
const AppLink = forwardRef<HTMLAnchorElement, LinkComponentProps>(({ href, to, ...rest }, ref) => {
  const target = href ?? to ?? "";
  if (/^[a-z]+:/i.test(target) || target.startsWith("#")) return <a ref={ref} href={target} {...rest} />;
  return <RouterLink ref={ref} to={target} {...rest} />;
});
AppLink.displayName = "AppLink";

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
