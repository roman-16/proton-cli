// @ts-check
import starlight from "@astrojs/starlight";
import { defineConfig, fontProviders } from "astro/config";
import starlightLinksValidator from "starlight-links-validator";
import starlightLlmsTxt from "starlight-llms-txt";
import starlightThemeBlack from "starlight-theme-black";

import { sidebar } from "./sidebar.ts";
import { description, origin, repo, social } from "./site.ts";

/*
 * The sidebar keeps its scroll position across a navigation.
 *
 * It is registered from a plugin rather than from `components`, because the
 * theme warns about any override it finds in the configuration - and this one is
 * the very thing that warning recommends, since it renders the theme's own
 * sidebar. Declared after the theme, it updates the same setting without being
 * seen.
 */
const sidebarScroll = {
  hooks: {
    "config:setup"({ config, updateConfig }) {
      updateConfig({
        components: { ...config.components, Sidebar: "./src/components/Sidebar.astro" },
      });
    },
  },
  name: "proton-cli-sidebar-scroll",
};

export default defineConfig({
  fonts: [
    {
      cssVariable: "--font-inter",
      name: "Inter",
      provider: fontProviders.fontsource(),
    },
  ],
  integrations: [
    starlight({
      customCss: ["./src/styles/proton.css"],
      description,
      /*
       * Half of what is shown here is terminal output, and a table that wraps
       * is a table destroyed: the columns a reader is matching up land under
       * each other. So a long line scrolls, and every block keeps the shape it
       * has in a terminal.
       *
       * A crontab has no grammar to highlight it with, and a schedule is five
       * fields and a command either way.
       */
      expressiveCode: {
        shiki: { langAlias: { cron: "txt" } },
      },
      favicon: "/favicon.svg",
      /*
       * Starlight declares a large summary card and leaves the image to the
       * site, so without these every share of every page renders an empty
       * rectangle.
       */
      head: [
        { attrs: { content: social, property: "og:image" }, tag: "meta" },
        { attrs: { content: "1200", property: "og:image:width" }, tag: "meta" },
        { attrs: { content: "630", property: "og:image:height" }, tag: "meta" },
        {
          attrs: {
            content: "proton-cli, your CLI for Proton Mail, Drive, Calendar, Pass and Contacts",
            property: "og:image:alt",
          },
          tag: "meta",
        },
        { attrs: { content: social, name: "twitter:image" }, tag: "meta" },
      ],
      lastUpdated: true,
      logo: { alt: "", src: "./src/assets/logo.svg" },
      plugins: [
        starlightThemeBlack({
          /*
           * A page here is a command line and a paragraph about it. Handing
           * either to somebody else's chat window is not what a reader came for,
           * and the control costs a button on every page to offer it.
           */
          docs: { showMarkdownActions: false },
          navLinks: [
            { label: "Docs", link: "/install/" },
            { label: "Commands", link: "/about/commands/" },
            { label: "FAQ", link: "/about/faq/" },
            { label: "Stats", link: "/stats/" },
          ],
        }),
        starlightLlmsTxt({
          description,
          projectName: "proton-cli",
          /*
           * Somebody asking a model how to do something with this wants the
           * grammar before the catalogue: the shape is what makes the other two
           * hundred commands guessable, and the reference is what it then reads.
           */
          promote: ["index", "first-commands", "commands", "using/output", "about/faq"],
        }),
        starlightLinksValidator(),
        sidebarScroll,
      ],
      sidebar,
      social: [{ href: repo, icon: "github", label: "GitHub" }],
      title: "proton-cli",
    }),
  ],
  /*
   * Pages that were linked from elsewhere before their content found a better
   * home. Astro writes each as a redirecting stub, so an old bookmark lands on
   * the page that answers it rather than on a 404.
   */
  redirects: {
    "/about/why/": "/about/faq/",
    "/apps/account/": "/account/",
    "/apps/api/": "/api/",
    "/apps/calendar/": "/calendar/",
    "/apps/contacts/": "/contacts/",
    "/apps/drive/": "/drive/",
    "/apps/mail/": "/mail/",
    "/apps/pass/": "/pass/",
    "/commands/account/": "/account/",
    "/commands/api/": "/api/",
    "/commands/calendar/": "/calendar/",
    "/commands/contacts/": "/contacts/",
    "/commands/drive/": "/drive/",
    "/commands/mail/": "/mail/",
    "/commands/pass/": "/pass/",
    "/commands/self/": "/proton/",
    "/configuration/": "/using/settings/",
    "/design-notes/": "/about/faq/",
    "/faq/": "/about/faq/",
    "/getting-started/": "/first-commands/",
    "/how-it-works/": "/about/security/",
    "/human-verification/": "/help/troubleshooting/",
    "/installation/": "/install/",
    "/language/": "/commands/",
    "/limitations/": "/help/limits/",
    "/output/": "/using/output/",
    "/quickstart/": "/first-commands/",
    "/references/": "/commands/",
    "/scripting/": "/using/scripting/",
    "/troubleshooting/": "/help/troubleshooting/",
  },
  site: origin,
  trailingSlash: "always",
});
