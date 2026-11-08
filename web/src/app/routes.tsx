import type { RouteObject } from "react-router";
import { AppShell } from "../shell/AppShell";
import { NotFound } from "../shell/NotFound";
import { PageShell } from "../shell/PageShell";
import { RouteError } from "../shell/RouteError";
import { PAGES } from "./navigation";

export const routes: RouteObject[] = [
  {
    path: "/",
    element: <AppShell />,
    errorElement: <RouteError />,
    children: [
      {
        // Page errors render inside the shell so navigation keeps working.
        errorElement: <RouteError />,
        children: [
          ...Object.values(PAGES).map((p) =>
            p.path === "/"
              ? { index: true, element: <PageShell page={p.key} /> }
              : { path: p.path.slice(1) + "/*", element: <PageShell page={p.key} /> },
          ),
          ...(import.meta.env.DEV
            ? [
                {
                  path: "__dev/components",
                  lazy: () => import("../dev/ComponentGallery").then((m) => ({ Component: m.ComponentGallery })),
                },
              ]
            : []),
          { path: "*", element: <NotFound /> },
        ],
      },
    ],
  },
];
