/** Opens the ⌘K search from anywhere (for example the 404 page's search button). */
export const OPEN_SEARCH_EVENT = "hx:open-search";

export function openSearch(): void {
  window.dispatchEvent(new Event(OPEN_SEARCH_EVENT));
}
