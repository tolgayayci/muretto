// Serves the static landing page from ./public. http and www redirect to
// https://muretto.ai, and every response gets the security headers below.
// No analytics.

const APEX = 'muretto.ai';

const HEADERS = {
  'Content-Security-Policy': [
    "default-src 'self'",
    "script-src 'self' 'unsafe-inline'",
    "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
    "font-src 'self' https://fonts.gstatic.com",
    "img-src 'self' data: https:",
    "connect-src 'self' https:",
    "form-action 'self' https: mailto:",
    "frame-ancestors 'none'",
    "base-uri 'none'",
    "object-src 'none'",
  ].join('; '),
  'Strict-Transport-Security': 'max-age=31536000',
  'X-Content-Type-Options': 'nosniff',
  'X-Frame-Options': 'DENY',
  'Referrer-Policy': 'strict-origin-when-cross-origin',
  'Permissions-Policy': 'camera=(), microphone=(), geolocation=(), browsing-topics=()',
};

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const live = url.hostname === APEX || url.hostname === `www.${APEX}`;
    if (live && (url.hostname !== APEX || url.protocol === 'http:')) {
      url.hostname = APEX;
      url.protocol = 'https:';
      return Response.redirect(url.toString(), 301);
    }
    const res = await env.ASSETS.fetch(request);
    const out = new Response(res.body, res);
    for (const [k, v] of Object.entries(HEADERS)) out.headers.set(k, v);
    // no-transform stops Cloudflare from rewriting the page, including injecting its analytics beacon.
    const cc = out.headers.get('Cache-Control');
    out.headers.set('Cache-Control', cc ? `${cc}, no-transform` : 'no-transform');
    return out;
  },
};
