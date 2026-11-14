import { PAGES } from "../app/navigation";

export const PROJECTS_PATH = PAGES.projects.path;
export const projectHref = (id: string) => `${PROJECTS_PATH}/${encodeURIComponent(id)}`;
export const envHref = (id: string, envId: string) => `${projectHref(id)}/${encodeURIComponent(envId)}`;
/** The unit's Compose editor (import or replace). */
export const editHref = (id: string, envId: string) => `${envHref(id, envId)}/compose`;
