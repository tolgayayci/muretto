import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { loadModel } from "@/lib/data";
import { sessionById, sessionResources } from "@/lib/model";
import { cleanId, facts } from "../_components/derive";
import { CreatedCards, CreatedSection, Header, HooksCard, NothingMade, OpenSection, Stats, Timeline } from "../_components/parts";
import css from "../_components/session.module.css";

type Props = { params: Promise<{ id: string }> };

async function find(params: Props["params"]) {
  const id = cleanId((await params).id);
  if (!id) return undefined;
  const m = await loadModel();
  const s = sessionById(m, id);
  return s ? { m, s } : undefined;
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const found = await find(params).catch(() => undefined);
  return { title: found ? `Session ${found.s.short}` : "Session" };
}

export default async function SessionPage({ params }: Props) {
  const found = await find(params);
  if (!found) notFound();
  const { m, s } = found;
  const f = facts(m, s, sessionResources(m, s));
  const made = f.rs.length > 0;
  const issues = f.open.length + f.cantTell.length + (s.live ? f.unchecked.length : 0) > 0;
  const cards = made && f.gone.length > 0 && f.rs.length <= 4 && !issues;

  return (
    <div className="m-page" style={{ gap: 32 }}>
      <nav aria-label="Breadcrumb" className={css.crumb}>
        <Link href="/">Overview</Link> / session {s.short}
      </nav>
      <Header m={m} f={f} />
      {s.transcriptOnly ? <HooksCard /> : null}
      {made ? <Stats m={m} f={f} /> : <NothingMade m={m} f={f} />}
      {made && issues ? <OpenSection m={m} f={f} /> : null}
      {made ? cards ? <CreatedCards m={m} f={f} /> : <CreatedSection m={m} f={f} /> : null}
      <Timeline f={f} />
    </div>
  );
}
