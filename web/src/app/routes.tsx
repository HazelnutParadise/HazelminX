import type { RouteObject } from "react-router";
import { AccountSecurityPage } from "../account/AccountSecurityPage";
import { AuthLayout } from "../auth/AuthLayout";
import { EnrollPage } from "../auth/EnrollPage";
import { LoginPage } from "../auth/LoginPage";
import { AUTH_PATHS } from "../auth/nextPath";
import { RequireAuth } from "../auth/RequireAuth";
import { SetupPage } from "../auth/SetupPage";
import { AddNodePage } from "../nodes/AddNodePage";
import { NodePage } from "../nodes/NodePage";
import { NodesPage } from "../nodes/NodesPage";
import { ProjectPage } from "../projects/ProjectPage";
import { ProjectsPage } from "../projects/ProjectsPage";
import { AppShell } from "../shell/AppShell";
import { SessionFallback } from "../shell/SessionFallback";
import { NotFound } from "../shell/NotFound";
import { PageShell } from "../shell/PageShell";
import { RouteError } from "../shell/RouteError";
import { PAGES } from "./navigation";

export const routes: RouteObject[] = [
  {
    element: <AuthLayout />,
    errorElement: <RouteError />,
    children: [
      { path: AUTH_PATHS.login.slice(1), element: <LoginPage /> },
      { path: AUTH_PATHS.setup.slice(1), element: <SetupPage /> },
      { path: AUTH_PATHS.enroll.slice(1), element: <EnrollPage /> },
    ],
  },
  {
    path: "/",
    element: (
      <RequireAuth fallback={(state) => <SessionFallback state={state} />}>
        <AppShell />
      </RequireAuth>
    ),
    errorElement: <RouteError />,
    children: [
      {
        // Page errors render inside the shell so navigation keeps working.
        errorElement: <RouteError />,
        children: [
          ...Object.values(PAGES)
            .filter((p) => p.key !== "accountSecurity" && p.key !== "nodes" && p.key !== "projects")
            .map((p) =>
              p.path === "/"
                ? { index: true, element: <PageShell page={p.key} /> }
                : { path: p.path.slice(1) + "/*", element: <PageShell page={p.key} /> },
            ),
          { path: PAGES.accountSecurity.path.slice(1), element: <AccountSecurityPage /> },
          { path: PAGES.nodes.path.slice(1), element: <NodesPage /> },
          { path: `${PAGES.nodes.path.slice(1)}/new`, element: <AddNodePage /> },
          { path: `${PAGES.nodes.path.slice(1)}/:id/*`, element: <NodePage /> },
          { path: PAGES.projects.path.slice(1), element: <ProjectsPage /> },
          { path: `${PAGES.projects.path.slice(1)}/:id/*`, element: <ProjectPage /> },
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
