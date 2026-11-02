import type { Metadata } from "next";
import Link from "next/link";
import { Icon } from "@/components/ds";
import { ProviderLogo, providerName } from "@/components/brand";
import { CommandChip } from "@/components/client";
import { Facet, type FacetOption } from "@/components/facet";
import { PageHeader } from "@/components/ui";
import { loadModel } from "@/lib/data";
import { age, ago, plural } from "@/lib/format";
import type { CheckGroup, Model } from "@/lib/model";
import { FILTERS, FILTER_LABEL, counts, emptyCopy, facets, groups, href, readParams, type Facets, type Params, type Raw } from "./_components/lib";
import { NewHere } from "./_components/new-here";
import { ProjectGroup } from "./_components/rows";
import s from "./_components/resources.module.css";

export const metadata: Metadata = { title: "My resources" };

const isAre = (n: number) => (n === 1 ? "is" : "are");
const DAY = 86_400_000;

export default async function ResourcesPage({ searchParams }: { searchParams: Promise<Raw> }) {
  const [m, raw] = await Promise.all([loadModel(), searchParams]);
  if (m.state === "new") return <NewHere />;
  const p = readParams(raw, m);
  const many = m.projects.length > 3;
  const failing = m.checks.filter((g) => !g.ok);
  const n = counts(m);
  const gs = groups(m, p, many);
  const waiting = new Set(m.waiting.map((r) => r.id));
  const firstCheck = m.state === "waiting";
  const clear = !firstCheck && m.open.length === 0 && m.cantTell.length === 0 && failing.length === 0;

  let title: string;
  let quiet: string;
  if (firstCheck) {
    title = `${plural(n.all, "resource")} ${n.all === 1 ? "was" : "were"} here before Muretto.`;
    quiet = n.open ? `First check done. ${n.open} ${isAre(n.open)} open to anyone.` : "First check done. Nothing is open to anyone.";
  } else if (failing.length) {
    title = `${plural(n.all, "resource")}, as of the last checks.`;
    quiet = failing.length === 1 ? `${providerName(failing[0].provider)} ${stalePhrase(failing[0], m)}.` : "Some providers couldn't be checked.";
  } else if (many) {
    title = `${plural(n.all, "resource")} in ${m.projects.length} projects.`;
    quiet = `${n.open ? `${n.open} ${isAre(n.open)}` : "None are"} open to anyone, ${n.waiting ? `${n.waiting} ${isAre(n.waiting)}` : "none are"} waiting on you.`;
  } else if (clear) {
    title = `${plural(n.all, "resource")} in your accounts.`;
    quiet = `None are open to anyone.${n.waiting ? ` ${n.waiting} ${isAre(n.waiting)} waiting on you.` : ""}`;
  } else {
    title = `${plural(n.all, "resource")} in your accounts.`;
    quiet = `${p.newest ? "Newest" : "Oldest"} first. ${n.open} ${isAre(n.open)} open to anyone.`;
  }

  const empty = gs.length === 0;
  const copy = emptyCopy(p, n.all);

  return (
    <div className="m-page" style={{ gap: 28 }}>
      <PageHeader eyebrow="My resources" title={title} quiet={quiet} right={<CheckedAt m={m} partly={failing.length > 0} />} />

      {failing.map((g) => (
        <CheckProblem key={`${g.provider}:${g.accountId}`} g={g} m={m} />
      ))}

      {firstCheck ? (
        <section aria-label="About these resources" style={banner}>
          <span className="m-tile dashed" style={{ width: 34, height: 34, background: "var(--surface-hover)" }}>
            <Icon name="question" size={16} />
          </span>
          <span style={bannerText}>
            <span style={{ fontSize: 15, fontWeight: 600 }}>Origin unknown: no recorded session made these.</span>
            <span style={bannerSub}>Muretto still checks whether they&apos;re open to anyone. They&apos;re never put up for review.</span>
          </span>
          {n.open === 0 ? (
            <span style={{ flex: "0 0 auto", display: "inline-flex", alignItems: "center", gap: 7, height: 28, padding: "0 11px", border: "1px solid var(--accent-line)", borderRadius: 999, background: "var(--accent-wash)", font: "500 12.5px var(--font-mono)", color: "var(--accent)" }}>✓ none open to anyone</span>
          ) : null}
        </section>
      ) : null}

      {clear ? (
        <section aria-label="Open to anyone" style={{ ...banner, borderColor: "var(--accent-line)", background: "var(--accent-wash)" }}>
          <span style={{ flex: "0 0 32px", height: 32, borderRadius: 999, background: "var(--accent)", color: "var(--on-accent)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 15, fontWeight: 600 }}>✓</span>
          <span style={bannerText}>
            <span style={{ fontSize: 15, fontWeight: 600 }}>Nothing here is open to anyone.</span>
            <span style={bannerSub}>
              Tables, buckets, previews and browser env vars in {plural(m.projects.length, "project")}
              {m.checkedAt ? `, checked ${ago(m.checkedAt, m.now)}` : ""}.
            </span>
          </span>
          <Link href="/providers" className="m-link-mono" style={{ flex: "0 0 auto" }}>
            What gets checked →
          </Link>
        </section>
      ) : null}

      <Toolbar p={p} f={facets(m, p)} />

      {empty ? (
        <div className="m-empty" style={{ minHeight: 560, padding: 40 }}>
          <Icon name="search" tile />
          <span className="m-empty-title" style={{ lineHeight: "24px" }}>
            {copy.title}
          </span>
          <span className="m-empty-text" style={{ maxWidth: 420 }}>
            {copy.text}
          </span>
          {copy.filters ? <span className="m-note-mono">{copy.filters}</span> : null}
          <div className={p.q && p.filter !== "all" ? "m-half" : "m-full"} style={{ width: "100%", maxWidth: 420, paddingTop: 10 }}>
            {p.q && p.filter !== "all" ? (
              <Link href={href(p, { q: "" })} className="mu-btn mu-btn-secondary">
                Clear search only
              </Link>
            ) : null}
            <Link href="/resources" className="mu-btn mu-btn-primary">
              {copy.showAll}
            </Link>
          </div>
        </div>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
          {gs.map((g) => (
            <ProjectGroup key={g.project.id} g={g} m={m} p={p} waiting={waiting} />
          ))}
        </div>
      )}

      {empty ? null : (
        <p style={{ margin: 0, display: "flex", alignItems: "center", gap: 8, fontSize: 13.5, lineHeight: "20px", color: "var(--ink-faint)" }}>
          <Icon name="lock" size={15} />
          Muretto only reads. To remove something, delete it at the provider.
        </p>
      )}
    </div>
  );
}

const banner = { display: "flex", alignItems: "center", gap: 14, padding: "16px 20px", border: "1px solid var(--line)", borderRadius: "var(--radius-xl)", background: "var(--surface-raised)" } as const;
const bannerText = { flexGrow: 1, display: "flex", flexDirection: "column", gap: 2, minWidth: 0 } as const;
const bannerSub = { fontSize: 13.5, lineHeight: "20px", color: "var(--ink-muted)" } as const;

function stalePhrase(g: CheckGroup, m: Model): string {
  if (!g.lastSuccessAt) return "hasn't been checked yet";
  const days = Math.floor((m.now.getTime() - g.lastSuccessAt.getTime()) / DAY);
  return days >= 1 ? `couldn't be checked for ${age(days)}` : `couldn't be checked since ${ago(g.lastSuccessAt, m.now)}`;
}

function CheckedAt({ m, partly }: { m: Model; partly: boolean }) {
  if (!m.checkedAt) return null;
  return (
    <span className="m-checked">
      <span className={`m-dot${partly ? " off" : ""}`} />
      {partly ? "partly checked" : "checked"} {ago(m.checkedAt, m.now)}
    </span>
  );
}

const PROBLEM: Record<string, { title: (n: string) => string; command?: string }> = {
  auth: { title: (n) => `${n} rejected your token`, command: "muretto init" },
  permission: { title: (n) => `${n} doesn't allow reading everything`, command: "muretto init" },
  rate_limit: { title: (n) => `${n} asked Muretto to slow down` },
};

function CheckProblem({ g, m }: { g: CheckGroup; m: Model }) {
  const name = providerName(g.provider);
  const code = g.failing[0]?.error_code ?? "";
  const pr = PROBLEM[code] ?? { title: (x: string) => `Couldn't check ${x}`, command: "muretto doctor" };
  const projects = m.projects.filter((p) => p.provider === g.provider && p.account === g.account).map((p) => p.name);
  const shown = projects.length === 1 ? projects[0] : projects.length ? `${name} projects` : "";
  const text = shown
    ? `${shown} below ${projects.length === 1 ? "shows" : "show"} the last good check${g.lastSuccessAt ? `, ${ago(g.lastSuccessAt, m.now)}` : ""}.`
    : "Results are from the last good check.";
  return (
    <section aria-label="Setup problem" style={{ ...banner, borderColor: "var(--line-strong)" }}>
      <span className="m-tile" style={{ width: 34, height: 34, color: "var(--ink)" }}>
        <Icon name="key" size={16} />
      </span>
      <span style={bannerText}>
        <span style={{ fontSize: 15, fontWeight: 600 }}>{pr.title(name)}</span>
        <span style={bannerSub}>{text}</span>
      </span>
      {pr.command ? <CommandChip command={pr.command} small prompt={false} /> : null}
    </section>
  );
}

const dot = <span className={s.dot} />;

function Toolbar({ p, f }: { p: Params; f: Facets }) {
  const reset = p.filter !== "all" || p.provider !== "" || p.project !== "" || p.q !== "";
  const providerOptions: FacetOption[] = [
    { key: "", href: href(p, { provider: "", expand: [] }), label: "All providers", count: f.providers.reduce((t, x) => t + x.count, 0), selected: !p.provider },
    ...f.providers.map(({ provider, count }) => ({
      key: provider,
      // A project on another provider doesn't survive the switch.
      href: href(p, { provider, project: f.projects.some((x) => x.project.name === p.project && x.project.provider === provider) ? p.project : "", expand: [] }),
      label: providerName(provider),
      mark: <ProviderLogo provider={provider} size={13} />,
      count,
      selected: p.provider === provider,
    })),
  ];
  const grouped = !p.provider && f.providers.length > 1;
  const projectOptions: FacetOption[] = [
    { key: "", href: href(p, { project: "", expand: [] }), label: p.provider ? `All ${providerName(p.provider)} projects` : "All projects", count: f.projects.reduce((t, x) => t + x.count, 0), selected: !p.project },
    ...f.projects.map(({ project: pr, count }) => ({
      key: pr.id,
      href: href(p, { project: pr.name, expand: [] }),
      label: <span style={{ font: "500 13px var(--font-mono)" }}>{pr.name}</span>,
      mark: pr.open > 0 ? dot : undefined,
      count,
      selected: p.project === pr.name,
      group: grouped ? (
        <>
          <ProviderLogo provider={pr.provider} size={12} />
          {providerName(pr.provider)}
        </>
      ) : undefined,
      groupKey: grouped ? pr.provider : undefined,
    })),
  ];
  const statusOptions: FacetOption[] = FILTERS.map((x) => ({
    key: x,
    href: href(p, { filter: x, expand: [] }),
    label: x === "all" ? "Any status" : FILTER_LABEL[x],
    mark: x === "open" && f.status.open > 0 ? dot : undefined,
    count: f.status[x],
    red: x === "open",
    selected: p.filter === x,
  }));
  const sortOptions: FacetOption[] = [
    { key: "old", href: href(p, { newest: false }), label: "Oldest first", selected: !p.newest },
    { key: "new", href: href(p, { newest: true }), label: "Newest first", selected: p.newest },
  ];
  return (
    <div className={s.toolbar}>
      <form action="/resources" method="get" role="search" className={s.search}>
        {p.filter !== "all" ? <input type="hidden" name="filter" value={p.filter} /> : null}
        {p.newest ? <input type="hidden" name="sort" value="new" /> : null}
        {p.provider ? <input type="hidden" name="provider" value={p.provider} /> : null}
        {p.project ? <input type="hidden" name="project" value={p.project} /> : null}
        <span className={s.searchIcon}>
          <Icon name="search" size={15} />
        </span>
        <input type="search" name="q" aria-label="Search resources" placeholder="Search names" defaultValue={p.q} maxLength={80} className={p.q ? s.filled : undefined} />
        {p.q ? (
          <Link href={href(p, { q: "" })} aria-label="Clear search" className={s.clear}>
            ×
          </Link>
        ) : null}
      </form>
      <div className={s.facets}>
        {f.providers.length > 1 ? <Facet label="Provider" active={!!p.provider} value={providerName(p.provider)} mark={p.provider ? <ProviderLogo provider={p.provider} size={12} /> : undefined} options={providerOptions} /> : null}
        {f.projects.length > 1 || p.project ? <Facet label="Project" active={!!p.project} value={<span style={{ fontFamily: "var(--font-mono)" }}>{p.project}</span>} options={projectOptions} /> : null}
        <Facet label="Status" active={p.filter !== "all"} value={FILTER_LABEL[p.filter]} mark={p.filter === "open" ? dot : undefined} options={statusOptions} />
        {reset ? (
          <Link href={href(p, { filter: "all", provider: "", project: "", q: "", expand: [] })} className={s.reset}>
            Reset
            <Icon name="x" size={13} />
          </Link>
        ) : null}
      </div>
      <div className={s.sort}>
        <Facet label={p.newest ? "Newest first" : "Oldest first"} active={false} value={null} options={sortOptions} align="end" />
      </div>
    </div>
  );
}
