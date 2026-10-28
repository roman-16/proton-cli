import type { StarlightUserConfig } from "@astrojs/starlight/types";

/*
 * Starlight's autogeneration sorts a section alphabetically, so the
 * collections are listed by hand, and scripts/import-docs.ts refuses to build
 * a site with a published page missing from here.
 */
export const sidebar: NonNullable<StarlightUserConfig["sidebar"]> = [
  {
    items: [
      { label: "Install", slug: "install" },
      { label: "First commands", slug: "first-commands" },
      { label: "How a command is built", slug: "commands" },
      { label: "Managing proton itself", slug: "proton" },
    ],
    label: "Start here",
  },
  {
    items: [
      { label: "Naming what to act on", slug: "using/naming" },
      { label: "Filters and bulk changes", slug: "using/filters" },
      { label: "Dry runs and confirmations", slug: "using/confirmations" },
      { label: "Output and exit codes", slug: "using/output" },
      { label: "Scripting", slug: "using/scripting" },
      { label: "AI agents", slug: "using/agents" },
      { label: "Settings, files and environment", slug: "using/settings" },
    ],
    label: "Everyday use",
  },
  {
    items: [
      { label: "Troubleshooting", slug: "help/troubleshooting" },
      { label: "What it can't do", slug: "help/limits" },
    ],
    label: "When something goes wrong",
  },
  {
    items: [
      { label: "Guide", slug: "account" },
      "account/account",
      "account/keys",
      "account/sessions",
      "account/security-log",
      "account/breaches",
      "account/profiles",
      "account/settings",
    ],
    label: "Account",
  },
  {
    items: [
      { label: "Guide", slug: "mail" },
      "mail/messages",
      "mail/conversations",
      "mail/drafts",
      "mail/mailing-lists",
      "mail/protected",
      "mail/settings",
    ],
    label: "Mail",
  },
  {
    items: [
      { label: "Guide", slug: "drive" },
      "drive/items",
      "drive/trash",
      "drive/photos",
      "drive/computers",
      "drive/sharing",
      "drive/links",
      "drive/shared",
      "drive/invitations",
      "drive/settings",
      "drive/volumes",
    ],
    label: "Drive",
  },
  {
    items: [
      { label: "Guide", slug: "calendar" },
      "calendar/events",
      "calendar/busy-times",
      "calendar/booking-pages",
      "calendar/reminders",
      "calendar/invitations",
      "calendar/settings",
    ],
    label: "Calendar",
  },
  {
    items: [
      { label: "Guide", slug: "pass" },
      "pass/items",
      "pass/vaults",
      "pass/folders",
      "pass/aliases",
      "pass/links",
      "pass/sharing",
      "pass/shared",
      "pass/invitations",
      "pass/trash",
      "pass/breaches",
      "pass/settings",
      { label: "import, export, generate", slug: "pass/pass" },
    ],
    label: "Pass",
  },
  {
    items: [{ label: "Guide", slug: "meet" }, "meet/meetings", "meet/rooms"],
    label: "Meet",
  },
  {
    items: [
      { label: "Guide", slug: "contacts" },
      "contacts/contacts",
      "contacts/emails",
      "contacts/groups",
      "contacts/keys",
    ],
    label: "Contacts",
  },
  {
    items: [{ label: "Guide", slug: "local-index" }, "local-index/commands"],
    label: "Local index",
  },
  {
    items: [{ label: "Guide", slug: "api" }, "api/api"],
    label: "Raw API",
  },
  {
    items: [
      { label: "FAQ", slug: "about/faq" },
      { label: "Security", slug: "about/security" },
      { label: "All commands", slug: "about/commands" },
      { label: "Changelog", slug: "changelog" },
      { label: "Proton API reference", link: "/api-reference/" },
    ],
    label: "About",
  },
];
