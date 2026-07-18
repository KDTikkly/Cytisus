import { enUS } from "@/i18n/en-US";

const statuses = [
  [enUS.statusLabel, enUS.statusValue],
  [enUS.apiLabel, enUS.apiValue],
  [enUS.providerLabel, enUS.providerValue],
  [enUS.safetyLabel, enUS.safetyValue],
] as const;

export default function Home() {
  return (
    <main>
      <section className="hero" aria-labelledby="page-title">
        <p className="eyebrow">{enUS.eyebrow}</p>
        <h1 id="page-title">{enUS.title}</h1>
        <p className="description">{enUS.description}</p>
      </section>

      <section className="status-grid" aria-label={enUS.statusLabel}>
        {statuses.map(([label, value]) => (
          <article className="status-card" key={label}>
            <p>{label}</p>
            <strong>{value}</strong>
          </article>
        ))}
      </section>

      <footer>{enUS.footer}</footer>
    </main>
  );
}
