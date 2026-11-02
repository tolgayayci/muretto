---
name: Provider request
about: Ask for a new provider adapter, or offer to write one
title: "Provider: "
labels: provider
---

<!--
Never paste tokens, real project ids, URLs or account data. Use fake names
such as example-org, customers or waitlist-preview.
-->

**Provider**
<!-- Name and website. -->

**API docs**
<!-- Link to the REST API reference. -->

**OpenAPI spec**
<!-- URL of an OpenAPI or Swagger file, if the provider publishes one.
     `muretto adapter new <file>` drafts an adapter from it. -->

**Read-only token**
<!-- The narrowest token that can list resources: its name, where it is
     created, and its permissions. If the provider has no read-only token,
     say what the narrowest one can also do. -->

**How do your agents create resources there?**
- [ ] The provider's CLI (name and command, e.g. `example db create`)
- [ ] An MCP server (name and link)
- [ ] The REST API directly (curl)
- [ ] Something else (Stripe Projects, a Terraform provider, a dashboard link):

<!-- If you can, paste what a successful create prints, with fake names. -->

**Example resources**
<!-- What agents leave behind, e.g. "a preview database called waitlist-preview
     and a storage bucket called customers". -->

**What would count as exposed?**
<!-- e.g. a public bucket, a database open to 0.0.0.0/0, a preview without
     protection. Link the docs for the field that says so, if you know it. -->

**Would you like to write the adapter?**
<!-- See adapters/README.md. You don't need a test account. -->
- [ ] Yes
- [ ] I can help test it against my own test account
