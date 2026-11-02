import Link from "next/link";
import { Icon } from "@/components/ds";

export default function ResourceNotFound() {
  return (
    <div className="m-center">
      <div role="alert" className="m-card m-alert">
        <span className="m-tile" style={{ width: 44, height: 44, borderRadius: 12, color: "var(--ink)" }}>
          <Icon name="search" size={20} />
        </span>
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <h1>
            <span>We can&apos;t find this resource.</span>
            <span className="q">It isn&apos;t in your accounts.</span>
          </h1>
          <span style={{ fontSize: 15, lineHeight: "23px", color: "var(--ink-muted)" }}>The link may be old, or it belongs to an account that isn&apos;t connected here. Everything Muretto sees is in My resources.</span>
        </div>
        <div className="m-full">
          <Link href="/resources" className="mu-btn mu-btn-primary">
            Back to My resources
          </Link>
        </div>
      </div>
    </div>
  );
}
