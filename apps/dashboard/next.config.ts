import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  devIndicators: false,
  async redirects() {
    return [
      { source: "/signin", destination: "/login", permanent: false },
      // The Review page was folded into My resources ("Waiting on you").
      { source: "/review", destination: "/resources?filter=waiting", permanent: false },
    ];
  },
};

export default nextConfig;
