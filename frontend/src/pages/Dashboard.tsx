import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, statusLabel } from '../api'
import NodeSelect, { useServerMode } from '../components/NodeSelect'
import RefreshBar, { useRefresh } from '../components/RefreshBar'
import { useBranding } from '../branding'
import HideFilters, { hideParam, useHideDefault } from '../components/HideFilters'

type Stats = {
  days: number
  totals: Record<string, number>
  series: { day: string; status: string; count: number }[]
  top_senders: { key: string; count: number }[]
  top_hosts: { key: string; count: number }[]
  top_recipient_domains: { key: string; count: number }[]
}

const statusColor: Record<string, string> = {
  delivered: 'var(--success)',
  failed: 'var(--danger)',
  deferred: 'var(--warning)',
  queued: 'var(--muted)',
  accepted: 'var(--info)',
}
const order = ['delivered', 'accepted', 'deferred', 'queued', 'failed']

function BarChart({ series }: { series: Stats['series'] }) {
  const days = useMemo(() => {
    const m = new Map<string, Record<string, number>>()
    for (const p of series) {
      const d = m.get(p.day) ?? {}
      d[p.status] = p.count
      m.set(p.day, d)
    }
    return [...m.entries()].sort(([a], [b]) => a.localeCompare(b))
  }, [series])
  if (!days.length) return <p className="muted">Sem dados no período.</p>
  const max = Math.max(...days.map(([, d]) => order.reduce((s, k) => s + (d[k] ?? 0), 0)), 1)
  const W = 640, H = 220, pad = 28
  const bw = Math.min((W - pad) / days.length, 80)
  return (
    <svg viewBox={`0 0 ${W} ${H + 24}`} width="100%" role="img" aria-label="Mensagens por dia e status">
      {[0, 0.5, 1].map((f) => (
        <g key={f}>
          <line x1={pad} x2={W} y1={H - f * H} y2={H - f * H} stroke="var(--border)" strokeDasharray="3 4" />
          <text x={0} y={H - f * H + 4} fill="var(--muted)" fontSize="10">{Math.round(max * f)}</text>
        </g>
      ))}
      {days.map(([day, d], i) => {
        let y = H
        const x = pad + i * bw + bw * 0.15 + ((W - pad) - bw * days.length) / 2
        return (
          <g key={day}>
            {order.map((k) => {
              const v = d[k] ?? 0
              if (!v) return null
              const h = (v / max) * H
              y -= h
              return (
                <rect key={k} x={x} y={y} width={bw * 0.7} height={h} rx={3} fill={statusColor[k]} opacity={0.9}>
                  <title>{`${day} · ${statusLabel[k]}: ${v}`}</title>
                </rect>
              )
            })}
            {(days.length <= 14 || i % Math.ceil(days.length / 10) === 0) && (
              <text x={x + bw * 0.35} y={H + 16} fill="var(--muted)" fontSize="10" textAnchor="middle">{day.slice(5)}</text>
            )}
          </g>
        )
      })}
    </svg>
  )
}

function Top({ title, items }: { title: string; items: { key: string; count: number }[] }) {
  const max = Math.max(...items.map((i) => i.count), 1)
  return (
    <div className="glass card bars">
      <h3>{title}</h3>
      {items.length === 0 && <p className="muted small">Sem dados.</p>}
      {items.map((i) => (
        <div className="bar-row" key={i.key}>
          <span className="lbl" title={i.key}>{i.key || '—'}</span>
          <div className="track"><div className="fill" style={{ width: `${(i.count / max) * 100}%` }} /></div>
          <span className="mono">{i.count}</span>
        </div>
      ))}
    </div>
  )
}

export default function Dashboard() {
  const server = useServerMode()
  const [node, setNode] = useState('')
  const [days, setDays] = useState(7)
  const hideDef = useHideDefault()
  const [hide, setHide] = useState(hideDef)
  const [s, setS] = useState<Stats | null>(null)
  const [err, setErr] = useState('')

  const { branding } = useBranding()
  const load = useCallback(async () => {
    try {
      setS(await api<Stats>(`/api/stats?days=${days}&hide=${hideParam(hide)}${node ? `&node=${node}` : ''}`))
      setErr('')
    } catch (e) {
      setErr((e as Error).message)
      throw e
    }
  }, [days, node, hide])
  const refresh = useRefresh(load)
  const { run } = refresh
  useEffect(() => { run() }, [load, run])

  const total = s ? Object.values(s.totals).reduce((a, b) => a + b, 0) : 0
  return (
    <>
      <div className="topbar">
        <h1>Painel</h1>
        <div className="row">
        {server && <NodeSelect value={node} onChange={setNode} />}
        <label className="f" style={{ width: 170 }}>Período
          <select value={days} onChange={(e) => setDays(Number(e.target.value))}>
            {[1, 7, 30, 90, 365].map((d) => <option key={d} value={d}>Últimos {d} {d === 1 ? 'dia' : 'dias'}</option>)}
          </select>
        </label>
        </div>
      </div>
      <RefreshBar r={refresh} tz={branding?.timezone} />
      <div style={{ marginBottom: 14 }}><HideFilters value={hide} onChange={setHide} /></div>
      {err && <div className="alert err">{err}</div>}
      {s && (
        <div className="stack">
          <div className="grid cols-4">
            <div className="glass stat"><div className="v">{total}</div><div className="l">Total de mensagens</div></div>
            {order.map((k) => (
              <div className="glass stat" key={k}>
                <div className="v" style={{ color: statusColor[k] }}>{s.totals[k] ?? 0}</div>
                <div className="l">{statusLabel[k]}</div>
              </div>
            ))}
          </div>
          <div className="glass card">
            <h2>Volume diário</h2>
            <BarChart series={s.series} />
            <div className="legend">
              {order.map((k) => <span key={k}><i style={{ background: statusColor[k] }} />{statusLabel[k]}</span>)}
            </div>
          </div>
          <div className="grid cols-2">
            <Top title="Principais remetentes (domínio)" items={s.top_senders} />
            <Top title="Principais origens" items={s.top_hosts} />
            <Top title="Principais destinos (domínio)" items={s.top_recipient_domains} />
          </div>
        </div>
      )}
    </>
  )
}
