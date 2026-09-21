import type { Endpoint } from "./types.js";

/**
 * Sources overlap, and they disagree on spelling: the same route is
 * `{shareID}` in one and `{shareId}` in another, which OpenAPI reads as two
 * routes. A route is therefore what is left once every placeholder is blanked,
 * and an operation is one method on it.
 *
 * Sources arrive most authoritative first. The first to declare an operation
 * defines it - its path spelling, its name, everything it says about the call -
 * and the rest only fill in what they alone know.
 */

export interface Route {
  url: string;
  operations: Map<string, Endpoint>;
}

export function merge(sources: Endpoint[][]): Route[] {
  const routes = new Map<string, Route>();

  for (const endpoints of sources) {
    for (const endpoint of endpoints) {
      const key = blankPlaceholders(endpoint.url);
      const route = routes.get(key) ?? { url: endpoint.url, operations: new Map() };
      routes.set(key, route);

      const defined = route.operations.get(endpoint.method);
      if (defined) {
        supplement(defined, endpoint);
        continue;
      }
      route.operations.set(endpoint.method, {
        ...endpoint,
        url: route.url,
        pathParams: placeholdersOf(route.url),
      });
    }
  }

  return [...routes.values()]
    .sort((a, b) => byCodeUnit(a.url, b.url))
    .map((route) => ({ url: route.url, operations: sortByMethod(route.operations) }));
}

function supplement(defined: Endpoint, also: Endpoint): void {
  defined.deprecated ||= also.deprecated;
  defined.isPublic ||= also.isPublic;
  defined.keepalive ||= also.keepalive;
  if (!defined.description) defined.description = also.description;
  if (!defined.summary) defined.summary = also.summary;
  if (!defined.timeout) defined.timeout = also.timeout;
  if (defined.silencedErrors.length === 0) defined.silencedErrors = also.silencedErrors;
}

function sortByMethod(operations: Map<string, Endpoint>): Map<string, Endpoint> {
  return new Map([...operations.entries()].sort((a, b) => byCodeUnit(a[0], b[0])));
}

// Code units, not the runtime's collation, so the same sources give the same
// file on every machine.
function byCodeUnit(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

function blankPlaceholders(url: string): string {
  return url.replace(/\{[^}]*\}/g, "{}");
}

function placeholdersOf(url: string): string[] {
  return [...url.matchAll(/\{([^}]+)\}/g)].map((m) => m[1]);
}
