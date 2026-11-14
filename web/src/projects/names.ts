/** Names Core accepts for projects, environments and units (spec 52.1): a lowercase letter first, then
 * lowercase letters, digits and "-" (not last), at most 30. */
export const NAME_PATTERN = /^[a-z](?:[a-z0-9-]{0,28}[a-z0-9])?$/;
