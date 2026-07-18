import { enUS } from "@/i18n/en-US";

const boundaries = [
  [enUS.accessLabel, enUS.accessValue],
  [enUS.dataLabel, enUS.dataValue],
  [enUS.actionsLabel, enUS.actionsValue],
  [enUS.auditLabel, enUS.auditValue],
] as const;

export default function AdminHome() {
  return (
    <main>
      <header>
        <p className="eyebrow">{enUS.eyebrow}</p>
        <h1>{enUS.title}</h1>
        <p className="description">{enUS.description}</p>
      </header>

      <section aria-label={enUS.title}>
        {boundaries.map(([label, value]) => (
          <article key={label}>
            <span>{label}</span>
            <strong>{value}</strong>
          </article>
        ))}
      </section>

      <footer>{enUS.footer}</footer>
    </main>
  );
}
