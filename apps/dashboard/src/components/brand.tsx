// Provider logos, provider/project badges, session avatars and kind icons.
import { Icon, type IconName } from "./ds";
import { blobBackground } from "@/lib/avatar";

export function SupabaseLogo({ size = 12 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 109 113" role="img" aria-label="Supabase">
      <path d="M63.708 110.284c-2.86 3.601-8.658 1.628-8.727-2.97l-1.007-67.251h45.22c8.19 0 12.758 9.46 7.665 15.875L63.708 110.284z" fill="#249361" />
      <path d="M45.317 2.071c2.86-3.601 8.657-1.628 8.726 2.97l.442 67.251H9.83c-8.19 0-12.759-9.46-7.665-15.875L45.317 2.071z" fill="#3ECF8E" />
    </svg>
  );
}

export function VercelLogo({ size = 11 }: { size?: number }) {
  return (
    <svg width={size} height={Math.round((size * 65) / 76)} viewBox="0 0 76 65" role="img" aria-label="Vercel">
      <path d="M37.527 0L75.055 65H0L37.527 0z" fill="#edece6" />
    </svg>
  );
}

/** One-colour marks on a 24×24 grid, from Simple Icons (CC0). Dark brand colours are drawn light. */
const MARKS: Record<string, { fill: string; d: string }> = {
  github: { fill: "#edece6", d: "M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" },
  cloudflare: { fill: "#F38020", d: "M16.5088 16.8447c.1475-.5068.0908-.9707-.1553-1.3154-.2246-.3164-.6045-.499-1.0615-.5205l-8.6592-.1123a.1559.1559 0 0 1-.1333-.0713c-.0283-.042-.0351-.0986-.021-.1553.0278-.084.1123-.1484.2036-.1562l8.7359-.1123c1.0351-.0489 2.1601-.8868 2.5537-1.9136l.499-1.3013c.0215-.0561.0293-.1128.0147-.168-.5625-2.5463-2.835-4.4453-5.5499-4.4453-2.5039 0-4.6284 1.6177-5.3876 3.8614-.4927-.3658-1.1187-.5625-1.794-.499-1.2026.119-2.1665 1.083-2.2861 2.2856-.0283.31-.0069.6128.0635.894C1.5683 13.171 0 14.7754 0 16.752c0 .1748.0142.3515.0352.5273.0141.083.0844.1475.1689.1475h15.9814c.0909 0 .1758-.0645.2032-.1553l.12-.4268zm2.7568-5.5634c-.0771 0-.1611 0-.2383.0112-.0566 0-.1054.0415-.127.0976l-.3378 1.1744c-.1475.5068-.0918.9707.1543 1.3164.2256.3164.6055.498 1.0625.5195l1.8437.1133c.0557 0 .1055.0263.1329.0703.0283.043.0351.1074.0214.1562-.0283.084-.1132.1485-.204.1553l-1.921.1123c-1.041.0488-2.1582.8867-2.5527 1.914l-.1406.3585c-.0283.0713.0215.1416.0986.1416h6.5977c.0771 0 .1474-.0489.169-.126.1122-.4082.1757-.837.1757-1.2803 0-2.6025-2.125-4.727-4.7344-4.727" },
  neon: { fill: "#34D59A", d: "M24 0V24l-9.365-8.045V24H0V0ZM2.942 21.087h8.751V9.563l9.365 8.204V2.919L2.942 2.914Z" },
  stripe: { fill: "#635BFF", d: "M13.976 9.15c-2.172-.806-3.356-1.426-3.356-2.409 0-.831.683-1.305 1.901-1.305 2.227 0 4.515.858 6.09 1.631l.89-5.494C18.252.975 15.697 0 12.165 0 9.667 0 7.589.654 6.104 1.872 4.56 3.147 3.757 4.992 3.757 7.218c0 4.039 2.467 5.76 6.476 7.219 2.585.92 3.445 1.574 3.445 2.583 0 .98-.84 1.545-2.354 1.545-1.875 0-4.965-.921-6.99-2.109l-.9 5.555C5.175 22.99 8.385 24 11.714 24c2.641 0 4.843-.624 6.328-1.813 1.664-1.305 2.525-3.236 2.525-5.732 0-4.128-2.524-5.851-6.594-7.305h.003z" },
  render: { fill: "#edece6", d: "M18.263.007c-3.121-.147-5.744 2.109-6.192 5.082-.018.138-.045.272-.067.405-.696 3.703-3.936 6.507-7.827 6.507-1.388 0-2.691-.356-3.825-.979a.2024.2024 0 0 0-.302.178V24H12v-8.999c0-1.656 1.338-3 2.987-3h2.988c3.382 0 6.103-2.817 5.97-6.244-.12-3.084-2.61-5.603-5.682-5.75" },
  planetscale: { fill: "#edece6", d: "M0 12C0 5.373 5.373 0 12 0c4.873 0 9.067 2.904 10.947 7.077l-15.87 15.87a11.981 11.981 0 0 1-1.935-1.099L14.99 12H12l-8.485 8.485A11.962 11.962 0 0 1 0 12Zm12.004 12L24 12.004C23.998 18.628 18.628 23.998 12.004 24Z" },
};

const NAMES: Record<string, string> = { supabase: "Supabase", vercel: "Vercel", github: "GitHub", cloudflare: "Cloudflare", neon: "Neon", stripe: "Stripe", render: "Render", planetscale: "PlanetScale" };

export function ProviderLogo({ provider, size = 12 }: { provider: string; size?: number }) {
  if (provider === "supabase") return <SupabaseLogo size={size} />;
  if (provider === "vercel") return <VercelLogo size={size - 1} />;
  const mark = MARKS[provider];
  if (mark) {
    return (
      <svg width={size} height={size} viewBox="0 0 24 24" role="img" aria-label={providerName(provider)}>
        <path d={mark.d} fill={mark.fill} />
      </svg>
    );
  }
  return <span aria-hidden>{provider.slice(0, 1).toUpperCase()}</span>;
}

export function providerName(provider: string): string {
  return NAMES[provider] ?? provider.charAt(0).toUpperCase() + provider.slice(1);
}

/** Square tile with the provider's logo, for card and group headers. */
export function ProviderTile({ provider, size = 36 }: { provider: string; size?: number }) {
  return (
    <span className="m-tile" style={{ width: size, height: size, borderRadius: size > 34 ? 10 : 8 }}>
      <ProviderLogo provider={provider} size={Math.round(size * 0.5)} />
    </span>
  );
}

export function ProviderBadge({ provider, large = false }: { provider: string; large?: boolean }) {
  return (
    <span className={`m-badge-pv${large ? " lg" : ""}`}>
      <ProviderLogo provider={provider} size={large ? 13 : 11} />
      {providerName(provider)}
    </span>
  );
}

export function ProjectBadge({ name, large = false }: { name: string; large?: boolean }) {
  return (
    <span className={`m-badge-pj${large ? " lg" : ""}`}>
      <svg width={large ? 13 : 11} height={large ? 13 : 11} viewBox="0 0 24 24" fill="none" stroke="var(--ink-faint)" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden>
        <path d="M12 3.5 20 8v8l-8 4.5L4 16V8z" />
        <path d="M4 8l8 4.5L20 8M12 12.5v8" />
      </svg>
      {name}
    </span>
  );
}

export function AssistantGlyph({ agent, size = 9 }: { agent: string; size?: number }) {
  if (agent === "codex") {
    return (
      <svg width={size} height={size} viewBox="0 0 16 16" fill="none" stroke="#edece6" strokeWidth={2.2} strokeLinecap="round" strokeLinejoin="round" role="img" aria-label="Codex">
        <path d="M3 4.5 7 8l-4 3.5M8.5 12h5" />
      </svg>
    );
  }
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" fill="none" stroke="#d97757" strokeWidth={2.4} strokeLinecap="round" role="img" aria-label="Claude Code">
      <path d="M8 1.5v13M1.5 8h13M3.4 3.4l9.2 9.2M12.6 3.4l-9.2 9.2" />
    </svg>
  );
}

export function assistantName(agent: string): string {
  return agent === "codex" ? "Codex" : "Claude Code";
}

/** The generated session avatar with the assistant's badge in the corner. */
export function SessionAvatar({ id, agent, size = 32 }: { id: string; agent: string; size?: number }) {
  const badge = size >= 40 ? 22 : size >= 28 ? 16 : 13;
  return (
    <span className="m-avatar" style={{ width: size, height: size }} title={assistantName(agent)}>
      <span className="m-avatar-blob" style={{ background: blobBackground(id) }} />
      <span className="m-avatar-agent" style={{ width: badge, height: badge }}>
        <AssistantGlyph agent={agent} size={Math.round(badge * 0.56)} />
      </span>
    </span>
  );
}

const KIND_ICON: Record<string, IconName | "table" | "bucket" | "preview" | "function" | "branch" | "project"> = {
  table: "table",
  bucket: "bucket",
  deployment: "preview",
  preview: "preview",
  function: "function",
  branch: "branch",
  project: "project",
  "env var": "key",
  domain: "globe",
};

/** Icon for a resource kind. */
export function KindIcon({ kind, size = 16 }: { kind: string; size?: number }) {
  const k = KIND_ICON[kind] ?? "database";
  const common = { width: size, height: size, viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: 1.8, strokeLinecap: "round" as const, strokeLinejoin: "round" as const, "aria-hidden": true };
  switch (k) {
    case "table":
      return <svg {...common}><rect x="3.5" y="4.5" width="17" height="15" rx="2" /><path d="M3.5 9.5h17M9.5 9.5v10" /></svg>;
    case "bucket":
      return <svg {...common}><ellipse cx="12" cy="6" rx="7.5" ry="2.5" /><path d="M4.5 6l1.6 12.2c.2 1.5 2.8 2.3 5.9 2.3s5.7-.8 5.9-2.3L19.5 6" /></svg>;
    case "preview":
      return <svg {...common}><rect x="3.5" y="4.5" width="17" height="15" rx="2" /><path d="M3.5 9h17M6.5 6.8h.01M9 6.8h.01" /></svg>;
    case "function":
      return <svg {...common}><path d="M8 4.5c-2 0-2.5 1-2.5 2.5v2c0 1.5-.8 2.5-2 3 1.2.5 2 1.5 2 3v2c0 1.5.5 2.5 2.5 2.5M16 4.5c2 0 2.5 1 2.5 2.5v2c0 1.5.8 2.5 2 3-1.2.5-2 1.5-2 3v2c0 1.5-.5 2.5-2.5 2.5" /></svg>;
    case "branch":
      return <svg {...common}><circle cx="6.5" cy="5.5" r="2" /><circle cx="6.5" cy="18.5" r="2" /><circle cx="17.5" cy="8" r="2" /><path d="M6.5 7.5v9M17.5 10c0 4-5 3.5-9.6 7" /></svg>;
    case "project":
      return <svg {...common}><path d="M12 3.5 20 8v8l-8 4.5L4 16V8z" /><path d="M4 8l8 4.5L20 8M12 12.5v8" /></svg>;
    default:
      return <Icon name={k as IconName} size={size} />;
  }
}
