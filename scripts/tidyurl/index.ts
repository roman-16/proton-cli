#!/usr/bin/env bun

/**
 * Writes the link-cleaning rules internal/tidyurl embeds, and the cases its
 * parity test holds the Go port to, from the TidyURL release Proton Mail's web
 * client cleans links with.
 *
 * The expected answers are the library's own, asked the way the web client asks
 * them, so a disagreement in the test is a disagreement with the web.
 *
 * Usage: bun tidyurl/index.ts WEBCLIENTS_DIR
 */

import { readFileSync, writeFileSync } from "fs";
import * as path from "path";
import { TidyURL } from "@protontech/tidy-url";
import rules from "@protontech/tidy-url/data/rules.js";
import pkg from "@protontech/tidy-url/package.json";

function webClientsVersion(dir: string): string {
  const lock = readFileSync(path.join(dir, "yarn.lock"), "utf8");
  const found = lock.match(/"@protontech\/tidy-url@npm:[^"]*":\s*\n\s*version: ([^\s]+)/);
  if (!found) {
    process.stderr.write("WebClients' yarn.lock names no @protontech/tidy-url\n");
    process.exit(1);
  }
  return found[1];
}

type Pattern = { source: string; flags: string };

function pattern(re: RegExp): Pattern {
  return { source: re.source, flags: re.flags };
}

function ruleJSON(rule: any) {
  return {
    name: rule.name,
    match: pattern(rule.match),
    match_href: rule.match_href === true,
    rules: rule.rules ?? [],
    replace: (rule.replace ?? []).map(pattern),
    exclude: (rule.exclude ?? []).map(pattern),
    redirect: rule.redirect ?? "",
    decode: rule.decode
      ? {
          param: rule.decode.param ?? "",
          look_for: rule.decode.lookFor ?? "",
          encoding: rule.decode.encoding ?? "",
          target_path: rule.decode.targetPath === true,
          handler: rule.decode.handler ?? "",
        }
      : null,
    allow: rule.allow ?? [],
    rev: rule.rev === true,
  };
}

// answer is what the web client makes of a link: Proton's own wrapper around
// the library, packages/shared/lib/mail/trackers.ts, restated.
function answer(link: string) {
  const fallback = { link, cleaned: link, tracked: false, removed: [] as { key: string; value: string }[] };
  try {
    try {
      if (!new URL(link).protocol.includes("http")) return fallback;
    } catch {
      return fallback;
    }
    TidyURL.config.allowAMP = true;
    TidyURL.config.silent = false;
    const { url, info } = TidyURL.clean(link);
    if (link.toLowerCase() !== url.toLowerCase()) {
      return { link, cleaned: url, tracked: true, removed: info.removed };
    }
    return fallback;
  } catch {
    return fallback;
  }
}

const hostOverrides: Record<string, string> = {
  "twitch.tv-email": "www.twitch.tv",
  "spotify.com": "open.spotify.com",
  "tiktok.com/link": "www.tiktok.com",
  mightyape: "www.mightyape.co.nz",
  "ampproject.org": "cdn.ampproject.org",
  "flexlinkspro.com": "track.flexlinkspro.com",
  "hbomax.com": "trk.hbomax.com",
  "justwatch.com": "click.justwatch.com",
  "knowyourmeme.com": "amp.knowyourmeme.com",
  "vi-control.net": "vi-control.net",
  "app.message.asce": "a.app.message.asce.org",
};

function hostFor(rule: any): string | null {
  const candidates = [hostOverrides[rule.name], rule.name, "www." + rule.name, "a." + rule.name].filter(Boolean);
  for (const host of candidates) {
    rule.match.lastIndex = 0;
    if (rule.match.test(rule.match_href ? `https://${host}/` : host)) return host;
  }
  return null;
}

const base64 = (s: string) => Buffer.from(s, "binary").toString("base64");
const landing = "https://example.com/landing?utm_medium=email&id=7";

function corpus(): string[] {
  const links: string[] = [];
  for (const rule of rules as any[]) {
    const host = hostFor(rule);
    if (!host) continue;
    const params = (rule.rules ?? []).map((key: string, i: number) => `${key}=v${i}`);
    links.push(`https://${host}/p/item?${[...params, "utm_source=news", "keep=1"].join("&")}`);
    links.push(`https://${host}/p/item?keep=1`);
    links.push(`https://${host}/`);
    for (const key of rule.allow ?? []) {
      links.push(`https://${host}/a?${key}=kept&utm_campaign=spring`);
    }
    if (rule.redirect) {
      links.push(`https://${host}/out?${rule.redirect}=${encodeURIComponent(landing)}`);
      links.push(`https://${host}/out?${rule.redirect}=${landing}`);
      links.push(`https://${host}/out?${rule.redirect.toUpperCase()}=${encodeURIComponent(landing)}&utm_term=x`);
      links.push(`https://${host}/out?${rule.redirect}=not%20a%20link&utm_term=x`);
      links.push(`https://${host}/out?${rule.redirect}=${encodeURIComponent(landing)}#section`);
    }
    if (rule.decode?.param && !rule.decode.handler) {
      const encoded = rule.decode.lookFor
        ? base64(JSON.stringify({ [rule.decode.lookFor]: landing }))
        : base64(landing);
      links.push(`https://${host}/c?${rule.decode.param}=${encodeURIComponent(encoded)}`);
      links.push(`https://${host}/c?${rule.decode.param}=${encoded}&utm_source=x`);
      links.push(`https://${host}/c?${rule.decode.param}=${encodeURIComponent(base64("null"))}`);
      links.push(`https://${host}/c?${rule.decode.param}=`);
    }
    if (rule.rev) {
      links.push(`https://${host}/n?a=&utm_source=x&b=1&c=`);
    }
  }
  return [...new Set([...links, ...handcrafted()])];
}

function handcrafted(): string[] {
  const reddit = encodeURIComponent("https://www.reddit.com/r/golang/comments/abc/?utm_source=share");
  const proofpoint = "https-3A__example.com_path_-3Futm-5Fsource-3Dx-26id-3D1&d=DwMF&c=x";
  return [
    "https://example.com/?utm_source=newsletter&utm_medium=email&utm_campaign=april",
    "https://EXAMPLE.com/Path?UTM_SOURCE=a&utm_source=b",
    "HTTPS://Example.COM/?fbclid=abc",
    "https://example.com/?utm_source=a#frag",
    "https://example.com/?utm_source=a#",
    "https://example.com/#?utm_source=a",
    "https://example.com/?",
    "https://example.com/?&&utm_source=a&&x=1",
    "https://example.com/?x&utm_source=a&y",
    "https://example.com/?x=a+b&y=%20c&utm_source=a",
    "https://example.com/?x=%zz&utm_source=a",
    "https://example.com/?x=ü&utm_source=a",
    "https://example.com/ö/ä?utm_source=a",
    "https://ÜBER.example/?utm_source=a",
    "https://xn--ber-goa.example/?utm_source=a",
    "https://user:pass@example.com/?utm_source=a",
    "https://example.com:443/?utm_source=a",
    "http://example.com:80/?utm_source=a",
    "https://example.com:8443/?utm_source=a",
    "https://127.0.0.1/?utm_source=a",
    "https://0x7f.1/?utm_source=a",
    "https://[2001:db8::1]/?utm_source=a",
    "https://example.com/a/./b/../c?utm_source=a",
    "https://example.com/a%2e%2E/b?utm_source=a",
    "https://example.com\\a\\b?utm_source=a",
    "https:example.com/?utm_source=a",
    "https://example.com/a b\"c<d>e`f{g}h^i|j'k?q=a b\"c<d>e`f{g}h^i|j'k&utm_source=a",
    "https://example.com/?q='x'&utm_source=a#a b\"c<d>e`f",
    "  https://example.com/?utm_source=a  ",
    "https://exa\tmple.com/?utm_\nsource=a",
    "https://example.com/?utm_source",
    "https://example.com/?utm_source=a&utm_source=b&x=1",
    "https://example.com/?utm_sourcex=a",
    "https://example.com/?%75tm_source=a",
    "https://example.com/?utm%5Fsource=a",
    "https://example.com/page?id=1",
    "https://example.com/",
    "https://example.com",
    "http://example.com/?gclid=abc&x=1",
    "ftp://example.com/?utm_source=a",
    "mailto:someone@example.com?subject=utm_source",
    "tel:+431234567",
    "javascript:alert(1)",
    "example.com/?utm_source=a",
    "//example.com/?utm_source=a",
    "https://",
    "https://exa mple.com/?utm_source=a",
    "https://example.com:99999/?utm_source=a",
    "",
    "undefined",
    "null",
    "https://www.amazon.com/dp/B0123/ref=sr_1_1?keywords=phone&tag=aff-20&th=1",
    "https://www.amazon.de/Some-Product/dp/B0123/ref=sr_1_1&ref_=abc?crid=x",
    "https://www.primevideo.com/detail/0ABC/ref=atv_hm?utm_source=x",
    "https://www.express.co.uk/news/world/123/story/amp",
    "https://news.artnet.com/art-world/story-123/amp-page",
    "https://www.facebook.com/sharer/sharer.php?u=https%3A%2F%2Fexample.com&fbclid=x",
    "https://www.facebook.com/page?fbclid=x&ref=y",
    "https://l.facebook.com/l.php?u=" + encodeURIComponent(landing) + "&h=AT0",
    "https://e.newsletters.cnn.com/click?utm_source=x&u=1",
    "https://www.youtube.com/watch?v=abc&feature=share&si=xyz",
    "https://www.youtube.com/redirect?q=" + encodeURIComponent(landing) + "&event=video",
    "https://www.google.com/url?url=" + encodeURIComponent(landing) + "&sa=t",
    "https://www.google.com/url?url=" + encodeURIComponent("https://www.youtube.com/watch?v=x&si=y"),
    "https://click.redditmail.com/CL0/" + reddit + "/1/abc",
    "https://click.redditmail.com/CL0/%E0%A4%A/1/abc",
    "https://click.redditmail.com/nothing",
    "https://urldefense.proofpoint.com/v2/url?u=" + proofpoint,
    "https://urldefense.proofpoint.com/v2/url?x=1",
    "https://patchbot.io/click/" + base64("a|b|" + encodeURIComponent(landing)),
    "https://patchbot.io/click/" + base64("a|b"),
    "https://stardockentertainment.info/lnk/" + base64("https://www.youtube.com/watch>v=abc"),
    "https://stardockentertainment.info/lnk/" + base64(landing) + "?x=1",
    "https://steam.gs/abc%3Eutm_source=x",
    "https://steam.gs/abc?x=1",
    "https://0yxjo.mjt.lu/lnk/AAA/" + base64(landing),
    "https://0yxjo.mjt.lu/lnk/AAA/" + base64(landing) + "?x=1",
    "https://deals.dominos.co.nz/x/" + base64(landing),
    "https://deals.dominos.co.nz/x/" + base64(landing) + "?utm_source=x",
    "https://go.redirectingat.com/?id=123&url=" + encodeURIComponent(landing) + "&sref=x",
    "https://go.redirectingat.com/?id=" + encodeURIComponent("123&url=" + landing),
    "https://go.redirectingat.com/?id=123&xs=1",
    "https://www.twitch.tv/r/e/" + base64(JSON.stringify({ name: "twitch_favorite_up", channel: "streamer" })) + "/x?utm_source=x",
    "https://www.twitch.tv/r/e/" + base64(JSON.stringify({ name: "other" })) + "/x?utm_source=x",
    "https://www.twitch.tv/r/e/notbase64!/x?utm_source=x",
    "https://www.emjcd.com/links?d=" + encodeURIComponent(base64("[1,2]")),
    "https://www.emjcd.com/links?d=" + encodeURIComponent(base64(JSON.stringify({ destinationUrl: 42 }))),
    "https://www.emjcd.com/links?d=" + encodeURIComponent(base64(JSON.stringify({ destinationUrl: landing + "#x" }))) + "#frag",
    "https://api.ffm.to/sl/x?cd=" + encodeURIComponent(base64(JSON.stringify({ destUrl: "not a link" }))),
    "https://stats.newswire.com/x?final=" + encodeURIComponent(base64("https://example.com/ü?utm_source=x")),
    "https://www.syteapi.com/x?url=" + base64(landing).replace(/=+$/, ""),
    "https://www.syteapi.com/x?url=a-b_c",
    "https://www.google.com/url?url=https://example.com/?a=1%26utm_source=x",
    "https://www.google.com/url?url=https://example.com/%zz",
    "https://www.google.com/url?url=%68ttps://example.com/?utm_source=x",
    "https://steamcommunity.com/linkfilter/?u=" + encodeURIComponent(landing),
    "https://www.linkedin.com/feed/?otpToken=abc&trk=x&utm_source=y",
    "https://newsletter.manor.ch/x?a=&b=1&utm_source=x",
    "https://www.fernsehlotterie.de/x?a=&utm_campaign=x",
  ];
}

function main() {
  const webClientsDir = process.argv[2];
  if (!webClientsDir) {
    process.stderr.write("usage: bun tidyurl/index.ts WEBCLIENTS_DIR\n");
    process.exit(1);
  }

  const pinned = webClientsVersion(webClientsDir);
  if (pinned !== pkg.version) {
    process.stderr.write(
      `WebClients cleans links with @protontech/tidy-url ${pinned}, scripts/package.json pins ${pkg.version}\n`
    );
    process.exit(1);
  }

  const out = path.join(import.meta.dir, "..", "..", "internal", "tidyurl");
  writeFileSync(path.join(out, "rules.json"), JSON.stringify(rules.map(ruleJSON), null, 2) + "\n");
  const cases = corpus().map(answer);
  writeFileSync(path.join(out, "testdata", "cases.json"), JSON.stringify(cases, null, 2) + "\n");
  process.stderr.write(
    `tidy-url ${pkg.version}: ${rules.length} rules, ${cases.length} cases (${cases.filter((c) => c.tracked).length} tracked)\n`
  );
}

main();
