import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { useSession } from "../auth/session";
import { nodeKeys, nodesApi, type NodeQuery } from "./api";
import { canDo, isUnreachable, NODES_WRITE } from "./status";

/** How often host lists are read again (the board's 「每 10 秒」). Status changes within 40 s on the Core side. */
export const REFRESH_MS = 10_000;

/** Every host matching `q`, read again every 10 s. */
export function useNodeList(q: NodeQuery = {}, enabled = true) {
  return useQuery({
    queryKey: nodeKeys.list(q),
    queryFn: ({ signal }) => nodesApi.list(q, signal),
    refetchInterval: REFRESH_MS,
    enabled,
  });
}

/**
 * Hosts that stopped answering, for the shell's host bar (DESIGN.md 主機狀態列). An account without nodes:read
 * gets an empty list from Core, and a failed read leaves the bar as it was rather than inventing an answer.
 */
export function useUnreachableHosts(): { id: string; name: string }[] | undefined {
  const list = useNodeList();
  return useMemo(
    () => list.data?.items.filter(isUnreachable).map((n) => ({ id: n.id, name: n.name || n.hostname || n.id })),
    [list.data],
  );
}

/**
 * Whether the signed-in user may change hosts (nodes:write), globally or on one host. Undefined while the
 * session or the role table is loading.
 */
export function useCanWriteNodes(nodeId?: string): boolean | undefined {
  const session = useSession();
  const roles = useQuery({ queryKey: nodeKeys.roles, queryFn: nodesApi.roles, staleTime: 5 * 60_000 });
  if (session.status !== "signed-in" || !roles.data) return undefined;
  return canDo(NODES_WRITE, session.session.assignments, roles.data.roles, nodeId);
}
