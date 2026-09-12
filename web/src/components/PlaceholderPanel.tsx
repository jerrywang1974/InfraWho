export function PlaceholderPanel({ title }: { title: string }) {
  return (
    <section className="panel placeholder-panel">
      <h2>{title}</h2>
      <p className="muted">即將在下一 PR 接上</p>
    </section>
  )
}
