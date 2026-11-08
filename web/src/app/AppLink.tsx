import type { LinkComponentProps } from "@cloudflare/kumo/utils";
import { forwardRef } from "react";
import { Link as RouterLink } from "react-router";

const EXTERNAL = /^(https?:|mailto:)/i;
const SCHEME = /^[a-z][a-z0-9+.-]*:/i;

/**
 * Link component for Kumo's LinkProvider. In-app paths go through React Router; http(s) and mailto
 * stay plain anchors; any other scheme (javascript:, data:, …) renders without an href, so a value that
 * came from outside can never run script.
 */
export const AppLink = forwardRef<HTMLAnchorElement, LinkComponentProps>(({ href, to, ...rest }, ref) => {
  const target = href ?? to ?? "";
  if (EXTERNAL.test(target) || target.startsWith("#")) return <a ref={ref} href={target} {...rest} />;
  if (SCHEME.test(target)) return <a ref={ref} {...rest} />;
  return <RouterLink ref={ref} to={target} {...rest} />;
});
AppLink.displayName = "AppLink";
