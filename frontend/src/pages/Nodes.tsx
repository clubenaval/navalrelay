import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { api, fmtDate } from '../api'
import { useDialog } from '../components/Dialog'
import { useBranding } from '../branding'

type Node = { id: number; name: string; disabled: boolean; created_at: string; last_seen: string | null; last_ip: string; version: string; kind: string; ver: string; outdated: boolean; messages: number }

export default function Nodes() {
  const { branding } = useBranding()
  const dlg = useDialog()
  const [list, setList] = useState<Node[]>([])
  const [name, setName] = useState('')
  const [token, setToken] = useState<{ name: string; token: string } | null>(null)
  const [msg, setMsg] = useState<{ t: 'ok' | 'err'; s: string } | null>(null)
  const [copied, setCopied] = useState(false)

  const load = useCallback(() => api<Node[]>('/api/admin/nodes').then(setList), [])
  useEffect(() => { load().catch((e) => setMsg({ t: 'err', s: e.message })) }, [load])

  const act = async (fn: () => Promise<unknown>, ok?: string) => {
    try {
      await fn()
      if (ok) setMsg({ t: 'ok', s: ok })
      await load()
    } catch (e) {
      setMsg({ t: 'err', s: (e as Error).message })
    }
  }

  const add = (e: FormEvent) => {
    e.preventDefault()
    setMsg(null)
    act(async () => {
      const r = await api<{ name: string; token: string }>('/api/admin/nodes', { method: 'POST', body: { name } })
      setToken(r)
      setName('')
    })
  }

  const rotate = async (n: Node) => (await dlg.confirm({ title: `Gerar novo token para "${n.name}"?`, message: 'O token atual deixa de funcionar e o servidor precisará ser atualizado com o novo.', danger: true, confirmLabel: 'Gerar novo token' })) &&
    act(async () => setToken({ name: n.name, token: (await api<{ token: string }>(`/api/admin/nodes/${n.id}/token`, { method: 'POST' })).token }))

  const copy = async (t: string) => {
    try { await navigator.clipboard.writeText(t) } catch {
      const ta = document.createElement('textarea')
      ta.value = t
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      ta.remove()
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  const host = typeof window !== 'undefined' ? window.location : null
  return (
    <>
      <div className="topbar"><h1>Servidores</h1></div>
      <p className="muted">Relays que enviam seus eventos para este servidor central. Cada servidor tem um token próprio, que pode ser revogado a qualquer momento.</p>
      <div className="glass card stack" style={{ margin: '12px 0' }}>
        <b>Como conectar um novo servidor</b>
        <ol className="guide">
          <li>Informe um nome abaixo e clique em <b>Adicionar servidor</b>. O token aparece <b>uma única vez</b>: copie-o.</li>
          <li>No NavalRelay do outro servidor, abra o assistente (ou Configurações &gt; Geral) e escolha a implantação <b>Cliente</b>.</li>
          <li>Preencha o endereço deste servidor{host ? <> (<span className="mono">{host.hostname}</span>)</> : null}, a porta e o protocolo. Cole o token e clique em <b>Testar conexão</b>.</li>
          <li>Se este servidor está atrás de um proxy reverso com HTTPS, use protocolo <b>HTTPS</b> e porta <b>443</b> no cliente, e ative "Estou atrás de um proxy reverso" em Configurações aqui.</li>
        </ol>
      </div>
      {branding?.deploy_mode !== 'server' && <div className="alert warn">Este sistema não está em modo Servidor: os clientes receberão "não encontrado". Altere a implantação em Configurações.</div>}
      {msg && <div className={`alert ${msg.t}`} role="status" style={{ margin: '12px 0' }}>{msg.s}</div>}
      {token && (
        <div className="glass card stack" style={{ margin: '12px 0' }}>
          <b>Token de “{token.name}” — copie agora, ele não será exibido novamente</b>
          <code className="block">{token.token}</code>
          <div className="row">
            <button className="sm" onClick={() => copy(token.token)}>{copied ? 'Copiado ✓' : 'Copiar token'}</button>
            <button className="sm" onClick={() => setToken(null)}>Já copiei</button>
          </div>
        </div>
      )}
      <form className="glass card row" onSubmit={add} style={{ marginBottom: 16 }}>
        <input style={{ flex: 1, minWidth: 220 }} placeholder="Nome do servidor (ex.: Filial São Paulo)" value={name} onChange={(e) => setName(e.target.value)} required aria-label="Nome do servidor" />
        <button className="primary">Adicionar servidor</button>
      </form>
      <div className="glass card">
        <div className="tablewrap">
          <table>
            <thead><tr><th>Servidor</th><th>Tipo</th><th>Status</th><th>Último contato</th><th>IP</th><th>Mensagens</th><th></th></tr></thead>
            <tbody>
              {list.map((n) => (
                <tr key={n.id}>
                  <td className="wrap"><b>{n.name}</b></td>
                  <td>{n.kind ? <><span className="badge b-info">{n.kind === 'cli' ? 'CLI' : 'App completo'}</span><div className="muted small mono">v{n.ver}{n.outdated && <span title="Versão anterior à do servidor central" style={{ color: 'var(--warn, #f5a524)' }}> ⚠ desatualizado</span>}</div></> : <span className="muted">—</span>}</td>
                  <td><span className={`badge ${n.disabled ? 'b-bad' : n.last_seen ? 'b-ok' : 'b-warn'}`}>{n.disabled ? 'desativado' : n.last_seen ? 'ativo' : 'aguardando'}</span></td>
                  <td className="mono" style={{ whiteSpace: 'nowrap' }}>{n.last_seen ? fmtDate(n.last_seen, branding?.timezone) : '—'}</td>
                  <td className="mono">{n.last_ip || '—'}</td>
                  <td>{n.messages}</td>
                  <td>
                    <div className="row">
                      <button className="sm" onClick={() => act(() => api(`/api/admin/nodes/${n.id}`, { method: 'PATCH', body: { disabled: !n.disabled } }))}>{n.disabled ? 'Ativar' : 'Desativar'}</button>
                      <button className="sm" onClick={() => rotate(n)}>Novo token</button>
                      <button className="sm danger" onClick={async () => (await dlg.confirm({ title: `Remover "${n.name}"?`, message: `As ${n.messages} mensagens recebidas dele também serão apagadas. Esta ação não pode ser desfeita.`, danger: true, confirmLabel: 'Remover' })) && act(() => api(`/api/admin/nodes/${n.id}`, { method: 'DELETE' }), 'Servidor removido')}>Remover</button>
                    </div>
                  </td>
                </tr>
              ))}
              {list.length === 0 && <tr><td colSpan={6} className="muted">Nenhum servidor cadastrado.</td></tr>}
            </tbody>
          </table>
        </div>
      </div>
    </>
  )
}
