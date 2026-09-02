/*
 * Turns the repository's documentation into the site's content collection.
 *
 * The pages are written for someone reading them on GitHub, and they stay that
 * way: no frontmatter is added to them, so nothing gains the key/value table
 * GitHub prints above it, and every relative link between them keeps working
 * there. Starlight needs a title in frontmatter and absolute links, so the two
 * are derived here instead - from the heading the page already has and from
 * where the link already points.
 *
 * The output is generated, so it is rebuilt from empty on every run and is not
 * committed. The markdown in the repository is the source of truth; this file is
 * the only thing that knows how it becomes a page.
 */
import { copyFile, mkdir, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { dirname, join, posix, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { branch, edit } from "../site.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const docs = join(root, "docs");
const content = join(root, "web/src/content/docs");
const assets = join(root, "web/src/assets");
const statics = join(root, "web/public");
const store = join(root, "web/.astro/data-store.json");

// docs/README.md is a table of contents, and the sidebar already is one.
const skipped = new Set(["docs/README.md"]);

/*
 * The changelog keeps the place at the root of the repository that GitHub,
 * `proton changelog` and the release workflow all read it from.
 */
const slugs = new Map([["CHANGELOG.md", "changelog"]]);

const alerts = new Map([
  ["CAUTION", "danger"],
  ["IMPORTANT", "note"],
  ["NOTE", "note"],
  ["TIP", "tip"],
  ["WARNING", "caution"],
]);

const markdownFiles = async (dir: string): Promise<string[]> => {
  const entries = await readdir(dir, { recursive: true, withFileTypes: true });
  return entries
    .filter((entry) => entry.isFile() && entry.name.endsWith(".md"))
    .map((entry) => posix.normalize(relative(root, join(entry.parentPath, entry.name))));
};

// Every page, as the path from the root of the repository that it was written at.
const sources = [...(await markdownFiles(docs)), "CHANGELOG.md"].filter((path) => !skipped.has(path)).sort();

const pages = new Set(sources);

/*
 * An app's directory holds its guide as a README, the way a reader on GitHub
 * expects, and the guide is the section itself rather than a page inside it. So
 * docs/mail/README.md is published at /mail/, beside the generated
 * docs/mail/messages.md at /mail/messages/.
 */
const slugOf = (path: string) =>
  slugs.get(path) ??
  path
    .replace(/^docs\//, "")
    .replace(/\.md$/, "")
    .replace(/(^|\/)README$/, "")
    .replace(/\/$/, "");

const pageURL = (path: string) => `/${slugOf(path)}/`;

/*
 * A link is resolved the way a reader on GitHub resolves it - against the
 * directory of the page it sits on - and then it is either another page here or
 * a file that only exists in the repository.
 */
const rewriteTarget = (from: string, target: string) => {
  if (/^(?:[a-z]+:|#|\/)/i.test(target)) return target;

  const [path, fragment] = target.split("#");
  if (!path) return target;

  const resolved = posix.normalize(posix.join(posix.dirname(from), path));
  const suffix = fragment ? `#${fragment}` : "";

  if (resolved === "docs/README.md") return `/${suffix}`;
  if (pages.has(resolved)) return `${pageURL(resolved)}${suffix}`;
  return `${branch}${resolved}${suffix}`;
};

const rewriteLinks = (from: string, body: string) =>
  body.replace(/(\]\()([^)\s]+)(\))/g, (_, open, target, close) => open + rewriteTarget(from, target) + close);

/*
 * GitHub writes a callout as a blockquote with a marker; Starlight writes it as
 * a directive. Same callout, two dialects.
 */
const rewriteAlerts = (body: string) =>
  body.replace(/^> \[!([A-Z]+)\]\n((?:>.*\n?)*)/gm, (whole, marker: string, quoted: string) => {
    const kind = alerts.get(marker);
    if (!kind) return whole;
    const text = quoted
      .split("\n")
      .map((line) => line.replace(/^>[ ]?/, ""))
      .join("\n")
      .trim();
    return `:::${kind}\n${text}\n:::\n`;
  });

/*
 * A page's lead is the paragraph between its heading and its first section, and
 * it is what the page is about - so it becomes the description and is lifted out
 * of the body, because the theme prints the description under the title and
 * leaving it in both places would say it twice.
 *
 * Only the lead qualifies. A page that opens straight into a section or an
 * example has no lead, and lifting a paragraph out of the middle of it would
 * take a sentence away from the section it belongs to.
 */
const lead = (body: string) => {
  const [above] = body.split(/^#{2,}\s/m);
  const paragraph = (above ?? "").split(/\n\s*\n/)[0]?.trim();
  if (!paragraph || /^[#>|\-*<:`]/.test(paragraph)) return undefined;

  const description = paragraph
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/[`*_]/g, "")
    .replace(/\s+/g, " ")
    .trim();
  return { description, paragraph };
};

const quote = (value: string) => `"${value.replace(/["\\]/g, "\\$&")}"`;

const render = (path: string, source: string) => {
  const heading = source.match(/^#\s+(.+)$/m);
  if (!heading?.[1]) throw new Error(`${path} has no heading to take a title from`);

  const withoutHeading = source.replace(/^#\s+.+\n+/m, "");
  const opening = lead(withoutHeading);
  const body = rewriteAlerts(
    rewriteLinks(path, opening ? withoutHeading.replace(opening.paragraph, "") : withoutHeading),
  );

  const frontmatter = [
    "---",
    `title: ${quote(heading[1].trim())}`,
    ...(opening ? [`description: ${quote(opening.description)}`] : []),
    // Where the page was written, which Starlight would otherwise guess at from
    // the path of the generated file it is reading here.
    `editUrl: ${quote(edit + path)}`,
    "---",
    "",
  ].join("\n");

  return frontmatter + body.trimStart();
};

/*
 * A collection entry is re-rendered when its own file changes, and code blocks
 * are rendered into it. So a syntax-highlighting option that moves in the
 * configuration reaches a build but not a running dev server, which then shows
 * blocks nobody wrote. The cache is dropped instead: it holds five files here,
 * and being able to trust the page is worth rebuilding them.
 */
const importDocs = async () => {
  await rm(store, { force: true });
  await rm(content, { force: true, recursive: true });
  for (const path of sources) {
    const out = join(content, `${slugOf(path)}.md`);
    await mkdir(dirname(out), { recursive: true });
    await writeFile(out, render(path, await readFile(join(root, path), "utf8")));
  }
};

/*
 * The logo is an import, so Starlight can hash and inline it. The demo panels
 * are served as they are: they are drawings of a terminal, already the size they
 * are meant to be shown at, and nothing an image pipeline does to an SVG of a
 * screenshot improves it. The social card is a PNG because that is the only
 * format the places a link gets pasted will draw.
 *
 * openapi.yaml is served byte for byte, at a URL a code generator or an HTTP
 * client can be pointed at. It is the file the repository holds and the file the
 * reference page reads, so there is one of it rather than three.
 */
const importAssets = async () => {
  await mkdir(assets, { recursive: true });
  await mkdir(statics, { recursive: true });
  await copyFile(join(root, "assets/logo.svg"), join(assets, "logo.svg"));
  await copyFile(join(root, "assets/logo.svg"), join(statics, "favicon.svg"));
  await copyFile(join(root, "openapi.yaml"), join(statics, "openapi.yaml"));
  for (const name of ["demo-dark.svg", "demo-light.svg", "og.png"]) {
    await copyFile(join(root, "assets", name), join(statics, name));
  }
};

await importDocs();
await importAssets();
