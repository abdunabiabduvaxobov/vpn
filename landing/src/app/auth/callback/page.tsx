import { completeOAuth } from "./exchange";

/**
 * /auth/callback (GET) — query-mode wrapper.
 *
 * Primary callback traffic flows through route.ts (POST) because both
 * Apple and Google use response_mode=form_post. This GET handler exists
 * for:
 *   - Operator hand-testing (`?provider=google&id_token=...&state=...`)
 *   - Provider fallbacks if form_post is ever switched off
 *   - Any future flow that includes `error` in the query
 *
 * The page is intentionally locale-LESS (D-10) so a single redirect URI
 * can be registered per provider. The post-exchange redirect adds the
 * locale prefix using the locale tunneled through the state payload.
 *
 * No visible chrome per 04-UI-SPEC §"/auth/callback" — Server Action
 * redirects before the placeholder paragraph hits the browser in 99.9%
 * of cases.
 */
export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type Props = {
  searchParams: Promise<{
    provider?: string;
    state?: string;
    id_token?: string;
    error?: string;
  }>;
};

export default async function AuthCallback({ searchParams }: Props) {
  const sp = await searchParams;
  const provider =
    sp.provider === "apple" || sp.provider === "google" ? sp.provider : null;

  if (!provider) {
    return (
      <main className="p-8">
        <p>Invalid callback.</p>
      </main>
    );
  }

  // completeOAuth always redirects (success or failure). The returned
  // JSX below is reached only if Next.js fails to throw — defensive
  // fallback content for a state we never expect to render.
  if (sp.error || !sp.id_token || !sp.state) {
    await completeOAuth({
      provider,
      idToken: "",
      state: sp.state ?? "",
    });
  } else {
    await completeOAuth({
      provider,
      idToken: sp.id_token,
      state: sp.state,
    });
  }

  return (
    <main className="p-8">
      <p>Completing sign-in…</p>
    </main>
  );
}
