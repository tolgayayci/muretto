import { Sidebar, type SidebarProps } from "@/components/sidebar";
import { initials } from "@/lib/auth";
import { loadModel, loadViewer } from "@/lib/data";
import { ago, clock, day, when } from "@/lib/format";

export const dynamic = "force-dynamic";

export default async function AppLayout({ children }: { children: React.ReactNode }) {
  const viewer = await loadViewer();
  let props: SidebarProps = {
    name: viewer.user.name,
    login: viewer.user.login,
    initials: initials(viewer.user.name),
    avatarUrl: viewer.user.avatarUrl,
    counts: { open: 0, resources: 0 },
    synced: null,
    connected: false,
    sessions: [],
  };
  try {
    const m = await loadModel();
    props = {
      ...props,
      counts: { open: m.open.length, resources: m.resources.filter((r) => r.existence !== "gone").length },
      synced: m.checkedAt ? `synced ${ago(m.checkedAt, m.now)}` : null,
      connected: m.hasProviders,
      sessions: m.sessions.slice(0, 6).map((s) => ({
        id: s.id,
        agent: s.agent,
        title: s.title,
        meta: `${s.short} · ${when(s.startedAt, m.now, undefined, false) === "today" ? clock(s.startedAt) : day(s.startedAt)}`,
        open: s.openCount > 0,
      })),
    };
  } catch {
    // The page shows the error; keep the shell usable.
  }
  return (
    <div className="m-shell">
      <Sidebar {...props} />
      <main className="m-main">{children}</main>
    </div>
  );
}
