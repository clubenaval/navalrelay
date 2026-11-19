import { useCallback, useEffect, useState } from 'react'
import { api, fmtDate, statusLabel } from '../api'
import { useBranding } from '../branding'
import NodeSelect, { useServerMode } from '../components/NodeSelect'
import RefreshBar, { useRefresh } from '../components/RefreshBar'
import ExportDialog from '../components/ExportDialog'
import HideFilters, { hideParam, useHideDefault } from '../components/HideFilters'

type Msg = {
  id: number
  exim_id: string
  received_at: string
  sender: string
  origin_host: string
  origin_ip: string
  subject: string
  size: number
  status: string
  recipients: string[]
  direction: string
  node: string
}
type Detail = {
  message: Msg
  deliveries: { at: string; recipient: string; status: string; remote_host: string; remote_ip: string; detail: string }[]
}

const empty = { q: '', from: '', to: '', status: '', sender: '', recipient: '', host: '', node: '', direction: '', hide: '' }

export default function Messages() {
  const { branding } = useBranding()
  const server = useServerMode()
  const tz = branding?.timezone
  const hideDef = useHideDefault()
  const base = { ...empty, hide: hideDef }
  const [f, setF] = useState(base)
  const [applied, setApplied] = useState(base)
  const [page, setPage] = useState(1)
  const [data, setData] = useState<{ total: number; size: number; items: Msg[]; hidden?: number } | null>(null)
  const [detail, setDetail] = useState<Detail | null>(null)
  const [err, setErr] = useState('')
  const [exp, setExp] = useState<null | 'pdf' | 'csv'>(null)

  const qs = useCallback(() => {
    const p = new URLSearchParams()
    Object.entries(applied).forEach(([k, v]) => k !== 'hide' && v && p.set(k, v))
    p.set('hide', hideParam(applied.hide))
    return p
  }, [applied])

  const load = useCallback(async () => {
    const p = qs()
    p.set('page', String(page))
    try {
      setData(await api<{ total: number; size: number; items: Msg[]; hidden?: number }>(`/api/messages?${p}`))
      setErr('')
    } catch (e) {
      setErr((e as Error).message)
      throw e
    }
  }, [qs, page])
  const refresh = useRefresh(load)
  const { run } = refresh
  useEffect(() => { run() }, [load, run])

  const open = async (id: number) => setDetail(await api<Detail>(`/api/messages/${id}`))
  const pages = data ? Math.max(1, Math.ceil(data.total / data.size)) : 1
  const set = (k: keyof typeof empty) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value })

  return (
    <>
      <div className="topbar">
        <h1>Mensagens</h1>
        <div className="row noprint">
          <button onClick={() => setExp('csv')}>⇩ Exportar CSV</button>
          <button onClick={() => setExp('pdf')}>⎙ Exportar PDF</button>
        </div>
      </div>
      <RefreshBar r={refresh} tz={tz} />
      <form
        className="glass card noprint"
        onSubmit={(e) => { e.preventDefault(); setPage(1); setApplied(f) }}
      >
        <div className="form-grid">
          <label className="f">Busca geral<input value={f.q} onChange={set('q')} placeholder="assunto, remetente, destinatário, ID" /></label>
          <label className="f">De<input type="date" value={f.from} onChange={set('from')} /></label>
          <label className="f">Até<input type="date" value={f.to} onChange={set('to')} /></label>
          <label className="f">Status
            <select value={f.status} onChange={set('status')}>
              <option value="">Todos</option>
              {Object.entries(statusLabel).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select>
          </label>
          {server && (
            <label className="f">Sentido
              <select value={f.direction} onChange={set('direction')}>
                <option value="">Todos</option>
                <option value="in">Recebidas</option>
                <option value="out">Enviadas</option>
              </select>
            </label>
          )}
          <label className="f">Remetente<input value={f.sender} onChange={set('sender')} /></label>
          <label className="f">Destinatário<input value={f.recipient} onChange={set('recipient')} /></label>
          <label className="f">Origem (host/IP)<input value={f.host} onChange={set('host')} /></label>
          {server && <NodeSelect value={f.node} onChange={(v) => setF({ ...f, node: v })} />}
        </div>
        <div style={{ marginTop: 14 }}><HideFilters value={f.hide} onChange={(v) => setF({ ...f, hide: v })} /></div>
        <div className="row" style={{ marginTop: 14 }}>
          <button className="primary">Filtrar</button>
          <button type="button" onClick={() => { setF(base); setApplied(base); setPage(1) }}>Limpar</button>
        </div>
      </form>
      {err && <div className="alert err" style={{ marginTop: 16 }}>{err}</div>}
      <div className="glass card" style={{ marginTop: 16 }}>
        <div className="row spread" style={{ marginBottom: 8 }}>
          <span className="muted small">{data ? `${data.total} mensagem(ns)${data.hidden ? ` · ${data.hidden} oculta(s) pelo filtro` : ''}` : 'Carregando…'}</span>
        </div>
        <div className="tablewrap">
          <table>
            <thead>
              <tr><th>Data</th>{server && <th>Servidor</th>}<th>Remetente</th><th>Destinatários</th><th>Assunto</th><th>Origem</th><th>Status</th></tr>
            </thead>
            <tbody>
              {data?.items.map((m) => (
                <tr key={m.id} className="click" onClick={() => open(m.id)}>
                  <td className="mono" style={{ whiteSpace: 'nowrap' }}>{fmtDate(m.received_at, tz)}</td>
                  {server && <td className="wrap">{m.node || 'Local'}</td>}
                  <td className="wrap">{m.direction === 'in' ? <span title="Recebida" style={{ color: 'var(--secondary)' }}>↓ </span> : m.direction === 'out' ? <span title="Enviada" style={{ color: 'var(--green, #34d399)' }}>↑ </span> : null}{m.sender || <span className="muted">Sem remetente</span>}</td>
                  <td className="wrap">{m.recipients.length ? <>{m.recipients.slice(0, 3).join(', ')}{m.recipients.length > 3 ? ` +${m.recipients.length - 3}` : ''}</> : <span className="muted">Sem destinatário</span>}</td>
                  <td className="wrap">{m.subject || <span className="muted">Sem assunto</span>}</td>
                  <td className="wrap mono">{m.origin_host || m.origin_ip ? <>{m.origin_host && m.origin_host !== m.origin_ip ? `${m.origin_host} ` : ''}{m.origin_ip}</> : <span className="muted">Sem origem</span>}</td>
                  <td><span className={`badge b-${m.status}`}>{statusLabel[m.status] ?? m.status}</span></td>
                </tr>
              ))}
              {data && data.items.length === 0 && <tr><td colSpan={server ? 7 : 6} className="muted">Nenhuma mensagem encontrada.</td></tr>}
            </tbody>
          </table>
        </div>
        <div className="row spread noprint" style={{ marginTop: 14 }}>
          <button disabled={page <= 1} onClick={() => setPage(page - 1)}>← Anterior</button>
          <span className="muted small">Página {page} de {pages}</span>
          <button disabled={page >= pages} onClick={() => setPage(page + 1)}>Próxima →</button>
        </div>
      </div>

      {exp && <ExportDialog initial={applied} kind={exp} onClose={() => setExp(null)} />}

      {detail && (
        <div className="modal-bg" onClick={() => setDetail(null)}>
          <div className="modal glass" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
            <div className="row spread">
              <h2>Detalhes da mensagem</h2>
              <button className="sm" onClick={() => setDetail(null)}>Fechar</button>
            </div>
            <div className="stack small">
              {server && <div><span className="muted">Servidor:</span> {detail.message.node || 'Local'}</div>}
              <div><span className="muted">ID:</span> <span className="mono">{detail.message.exim_id}</span></div>
              <div><span className="muted">Data:</span> {fmtDate(detail.message.received_at, tz)}</div>
              <div><span className="muted">Remetente:</span> {detail.message.sender || <span className="muted">Sem remetente</span>}</div>
              <div><span className="muted">Assunto:</span> {detail.message.subject || <span className="muted">Sem assunto</span>}</div>
              <div><span className="muted">Origem:</span> {detail.message.origin_host || detail.message.origin_ip ? <>{detail.message.origin_host} <span className="mono">{detail.message.origin_ip}</span></> : <span className="muted">Sem origem</span>}</div>
              <div><span className="muted">Tamanho:</span> {(detail.message.size / 1024).toFixed(1)} KB</div>
            </div>
            <h3 style={{ marginTop: 18 }}>Entregas</h3>
            <div className="tablewrap">
              <table>
                <thead><tr><th>Hora</th><th>Destinatário</th><th>Status</th><th>Servidor remoto</th><th>Detalhe</th></tr></thead>
                <tbody>
                  {detail.deliveries.map((d, i) => (
                    <tr key={i}>
                      <td className="mono">{fmtDate(d.at, tz)}</td>
                      <td className="wrap">{d.recipient}</td>
                      <td><span className={`badge b-${d.status}`}>{statusLabel[d.status] ?? d.status}</span></td>
                      <td className="wrap mono">{d.remote_host} {d.remote_ip}</td>
                      <td className="wrap small">{d.detail}</td>
                    </tr>
                  ))}
                  {detail.deliveries.length === 0 && <tr><td colSpan={5} className="muted">Sem registros de entrega.</td></tr>}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
