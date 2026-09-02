export const origin = "https://proton-cli.lerchster.dev";

export const repo = "https://github.com/roman-16/proton-cli";

export const branch = `${repo}/blob/main/`;

export const edit = `${repo}/edit/main/`;

/*
 * The counters recorded by the Stats workflow, read in the browser rather than
 * at build time so the page is as current as the last recording instead of as
 * current as the last deploy. raw.githubusercontent.com serves it with an open
 * CORS header and a five-minute cache; the API cannot be read from a browser at
 * all, because it rate-limits anonymous callers per address.
 */
export const stats = `https://raw.githubusercontent.com/roman-16/proton-cli/data/stats.json`;

export const social = `${origin}/og.png`;

export const tagline = "Your CLI for Proton Mail, Drive, Calendar, Pass and Contacts.";

export const description = `${tagline} One binary, end-to-end encrypted.`;
