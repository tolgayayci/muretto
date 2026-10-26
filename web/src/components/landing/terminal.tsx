// A styled terminal showing `muretto session <id>`: git log for infrastructure.
// Rendered on the server from the same sample Answer as the dashboard preview,
// laid out like the CLI's target output (docs/BRIEF.md §10).
import { kindLabel, providerLabel, sessionSpan, shortId } from "@/lib/format";
import { buildView, groupResources, headlineFinding, worstExposedName } from "@/lib/model";
import { SAMPLE_NOW, SAMPLE_SHOWCASE_SESSION, SAMPLE_TIME_ZONE, sampleTeamAnswer } from "@/lib/sample";

// Terminal colors are fixed: the terminal is dark in both themes.
const C = {
  text: "text-[#e9e8e2]",
  dim: "text-[#9d9c94]",
  faint: "text-[#75746c]",
  accent: "text-[#c8f169]",
  danger: "text-[#ff6b61]",
};

export function SessionTerminal() {
  const view = buildView(sampleTeamAnswer);
  const s = view.session.get(SAMPLE_SHOWCASE_SESSION)!;
  const now = Date.parse(SAMPLE_NOW);
  const id = shortId(s.session.id);
  const groups = groupResources(view, s.resource_ids);
  const exposedName = worstExposedName(view, s.resource_ids);
  const kindW = Math.max(...s.resource_ids.map((rid) => kindLabel(view.resource.get(rid)!.kind).length)) + 2;
  const nameW = Math.max(...s.resource_ids.map((rid) => view.resource.get(rid)!.name.length)) + 3;

  return (
    <figure className="overflow-hidden rounded-xl border border-[#2a2925] bg-term shadow-[0_30px_80px_-30px_rgb(0_0_0/0.65)]">
      <div className="flex h-9 items-center gap-2 border-b border-[#24231f] px-4">
        <span className="size-2.5 rounded-full bg-[#3a3934]" />
        <span className="size-2.5 rounded-full bg-[#3a3934]" />
        <span className="size-2.5 rounded-full bg-[#3a3934]" />
        <span className={`ml-3 font-mono text-[11px] ${C.faint}`}>~/acme/web</span>
      </div>
      <pre
        className={`scroll-thin overflow-x-auto px-5 py-4 font-mono text-[12px] leading-[1.7] sm:text-[12.5px] ${C.text}`}
        aria-label={`Terminal output of muretto session ${id}`}
      >
        <span className={C.faint}>$ </span>muretto session {id}
        {"\n\n"}
        <span className="font-medium">Session {id}</span>
        <span className={C.dim}>
          {" "}
          · {s.developer_name} · Claude Code · {sessionSpan(s.session.started_at, s.session.ended_at, now, SAMPLE_TIME_ZONE)}
        </span>
        {"\n"}
        <span className={C.dim}>Task: </span>“{s.session.task_title}”{"\n"}
        <span className={C.dim}>Repo: </span>
        {s.session.repo?.display_name} ({s.session.git_branch}){"\n"}
        {groups.map((g) => (
          <span key={`${g.provider}|${g.parentId ?? ""}`}>
            {"\n  "}
            <span className={`font-medium ${C.dim}`}>{providerLabel(g.provider).toUpperCase()}</span>
            {g.parent ? (
              <span className={C.faint}>
                {"  "}
                {kindLabel(g.parent.kind)} {g.parent.name}
              </span>
            ) : null}
            {"\n"}
            {g.resources.map((r) => {
              const f = headlineFinding(view, r.id);
              return (
                <span key={r.id}>
                  {"  "}
                  <span className={C.accent}>✓</span> <span className={C.dim}>{kindLabel(r.kind).padEnd(kindW)}</span>
                  {r.name.padEnd(nameW)}
                  <span className={C.accent}>exact</span>
                  {f?.state === "exposed" ? <span className={C.danger}>{`   ⚠ ${f.title}`}</span> : null}
                  {"\n"}
                </span>
              );
            })}
          </span>
        ))}
        {"\n  "}
        <span className={s.exposed_count > 0 ? C.danger : C.dim}>{s.exposed_count} exposed</span>
        <span className={C.dim}> · {s.review_due_count} need review</span>
        {exposedName ? (
          <>
            {"\n  "}
            <span className={C.faint}>→ muretto why {exposedName}   → muretto fix {exposedName}</span>
          </>
        ) : null}
        {"\n\n"}
        <span className={C.faint}>$ </span>
        <span className={`caret inline-block h-[1.1em] w-[0.55em] translate-y-[0.2em] bg-[#c8f169]/80`} aria-hidden="true" />
      </pre>
      <figcaption className="sr-only">
        Example output with sample data: one Claude Code session and every resource it created outside the repo.
      </figcaption>
    </figure>
  );
}
