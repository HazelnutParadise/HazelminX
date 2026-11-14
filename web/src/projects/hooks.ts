import { useQuery } from "@tanstack/react-query";
import { useSession } from "../auth/session";
import { nodeKeys, nodesApi } from "../nodes/api";
import { useNodeList } from "../nodes/hooks";

export const PROJECTS_WRITE = "projects:write";
export const PROJECTS_DEPLOY = "projects:deploy";

/**
 * Whether the signed-in user may do `action` on project `projectId` (a global or that project's assignment);
 * without an id, whether they may do it everywhere (creating a project). Undefined while loading. Core checks
 * again on every call: this only decides how buttons are drawn.
 */
export function useProjectPermission(action: string, projectId?: string): boolean | undefined {
  const session = useSession();
  const roles = useQuery({ queryKey: nodeKeys.roles, queryFn: nodesApi.roles, staleTime: 5 * 60_000 });
  if (session.status !== "signed-in" || !roles.data) return undefined;
  const grants = new Map(roles.data.roles.map((r) => [r.role, r.actions]));
  return session.session.assignments.some((a) => {
    const covers =
      a.scope.kind === "global" || (projectId !== undefined && a.scope.kind === "project" && a.scope.id === projectId);
    const actions = grants.get(a.role) ?? [];
    return covers && (actions.includes("*") || actions.includes(action));
  });
}

/** Whether the signed-in user is a global owner (plans whose approval needs the owner). */
export function useIsOwner(): boolean | undefined {
  const session = useSession();
  if (session.status !== "signed-in") return undefined;
  return session.session.assignments.some((a) => a.role === "owner" && a.scope.kind === "global");
}

/** Name of a host id, from the host list (the id itself until the list arrives). */
export function useHostName(): (id: string | null | undefined) => string {
  const nodes = useNodeList();
  return (id) => {
    if (!id) return "—";
    const n = nodes.data?.items.find((x) => x.id === id);
    return n ? n.name || n.hostname || n.id : id;
  };
}
