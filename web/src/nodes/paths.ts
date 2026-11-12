import { PAGES } from "../app/navigation";

export const NODES_PATH = PAGES.nodes.path;
export const nodeHref = (id: string) => `${NODES_PATH}/${encodeURIComponent(id)}`;
export const ADD_NODE_PATH = `${NODES_PATH}/new`;
