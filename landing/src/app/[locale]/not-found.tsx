import { useTranslations } from "next-intl";
import { Link } from "@/i18n/navigation";

export default function NotFound() {
  const t = useTranslations("notFound");
  return (
    <main className="flex min-h-screen flex-col items-center justify-center px-6 text-center">
      <p className="font-mono text-sm text-primary">404</p>
      <h1 className="mt-4 font-heading text-4xl font-semibold md:text-5xl">
        {t("title")}
      </h1>
      <p className="mt-4 max-w-md text-muted-foreground">{t("description")}</p>
      <Link
        href="/"
        className="mt-8 rounded-full bg-primary px-6 py-3 text-sm font-medium text-primary-foreground transition hover:bg-primary-glow"
      >
        {t("cta")}
      </Link>
    </main>
  );
}
