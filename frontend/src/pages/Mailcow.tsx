import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { api, fmtDate } from '../api'
import { useDialog } from '../components/Dialog'
import { useBranding } from '../branding'

type Srv = { id: number; name: string; url: string; disabled: boolean; skip_verify: boolean; last_sync: string | null; last_error: string; version: string; messages: number }
const blank = { name: '', url: '', api_key: '', skip_verify: false }

export default function Mailcow() {
  const { branding, refresh } = useBranding()
  const dlg = useDialog()
  const [list, setList] = useState<Srv[]>([])
  const [f, setF] = useState(blank)
  const [editing, setEditing] = useState<Srv | null>(null)
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState<{ t: 'ok' | 'err'; s: string } | null>(null)

  const load = useCallback(() => api<Srv[]>('/api/admin/mailcow').then(setList), [])
  useEffect(() => { load().catch((e) => setMsg({ t: 'err', s: e.message })) }, [load])
  useEffect(() => {
    const t = setInterval(() => { load().catch(() => {}) }, 15000)
    return () => clearInterval(t)
  }, [load])

  const run = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true)
    setMsg(null)
    try {
      await fn()
      setMsg({ t: 'ok', s: ok })
      await load()
      await refresh()
    } catch (e) {
      setMsg({ t: 'err', s: (e as Error).message })
    } finally {
      setBusy(false)
    }
  }

  const body = { name: f.name, url: f.url, api_key: f.api_key, skip_verify: f.skip_verify }
  const test = () => run(async () => {
    const r = await api<{ version: string }>('/api/admin/mailcow/test', { method: 'POST', body: editing ? { ...body, name: editing.name } : body })
    setMsg({ t: 'ok', s: `Conexão bem-sucedida. Mailcow ${r.version || ''}`.trim() })
  }, 'Conexão bem-sucedida')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (editing) {
      run(async () => { await api(`/api/admin/mailcow/${editing.id}`, { method: 'PUT', body }); setEditing(null); setF(blank) }, 'Servidor atualizado')
    } else {
      run(async () => { await api('/api/admin/mailcow', { method: 'POST', body }); setF(blank) }, 'Servidor adicionado. A primeira sincronização começa em instantes.')
    }
  }
  const edit = (s: Srv) => { setEditing(s); setF({ name: s.name, url: s.url, api_key: '', skip_verify: s.skip_verify }); window.scrollTo({ top: 0, behavior: 'smooth' }) }

  return (
    <>
      <div className="topbar"><h1>Servidores Mailcow</h1></div>
      <p className="muted">Registra as mensagens recebidas e enviadas por servidores Mailcow, lendo a API deles (somente leitura). O NavalRelay consulta o Mailcow a cada 30 segundos; nada é instalado nem alterado no servidor Mailcow.</p>
      <div className="glass card stack" style={{ margin: '12px 0' }}>
        <b>Como conectar um Mailcow</b>
        <ol className="guide">
          <li>No painel do Mailcow, abra <b>Sistema &gt; Configuração &gt; Acesso &gt; API</b> (em versões antigas: Configuração &gt; Acesso).</li>
          <li>Ative <b>Habilitar acesso à API</b> e, em <b>Permitir acesso à API a partir destes IPs/redes</b>, informe o IP público deste NavalRelay. Sem isso o Mailcow responde “acesso negado”.</li>
          <li>Em <b>Chave de API somente leitura</b>, gere uma chave e copie-a. Use sempre a chave de leitura, nunca a de leitura e escrita.</li>
          <li>Informe abaixo o endereço do Mailcow (ex.: <span className="mono">https://mail.exemplo.com.br</span>) e cole a chave. Use <b>Testar conexão</b> antes de salvar.</li>
        </ol>
        <span className="help">Direção: mensagens autenticadas (usuário do Mailcow) contam como <b>enviadas</b>; as demais, como <b>recebidas</b>. O histórico disponível é o que o Mailcow mantém em seus logs (algumas milhares de linhas).</span>
      </div>
      {msg && <div className={`alert ${msg.t}`} role="status" style={{ margin: '12px 0' }}>{msg.s}</div>}
      <form className="glass card stack" onSubmit={submit} style={{ marginBottom: 16 }}>
        <b>{editing ? `Editar “${editing.name}”` : 'Adicionar servidor Mailcow'}</b>
        <div className="form-grid">
          <label className="f">Nome<input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="Ex.: Mailcow Matriz" required disabled={!!editing} /></label>
          <label className="f">Endereço do Mailcow<input value={f.url} onChange={(e) => setF({ ...f, url: e.target.value })} placeholder="https://mail.exemplo.com.br" required /></label>
          <label className="f">Chave de API (somente leitura)
            <input type="password" autoComplete="off" value={f.api_key} onChange={(e) => setF({ ...f, api_key: e.target.value })} placeholder={editing ? 'Deixe em branco para manter a atual' : 'xxxxxx-xxxxxx-xxxxxx-xxxxxx-xxxxxx'} required={!editing} />
          </label>
        </div>
        <label className="row"><input type="checkbox" checked={f.skip_verify} onChange={(e) => setF({ ...f, skip_verify: e.target.checked })} /> Não validar o certificado TLS (somente para certificado autoassinado)</label>
        <div className="row">
          <button type="button" onClick={test} disabled={busy || !f.url || (!editing && !f.api_key)}>Testar conexão</button>
          <button className="primary" disabled={busy}>{editing ? 'Salvar alterações' : 'Adicionar servidor'}</button>
          {editing && <button type="button" onClick={() => { setEditing(null); setF(blank) }}>Cancelar</button>}
        </div>
      </form>
      <div className="glass card">
        <div className="tablewrap">
          <table>
            <thead><tr><th>Servidor</th><th>Status</th><th>Última sincronização</th><th>Mensagens</th><th></th></tr></thead>
            <tbody>
              {list.map((s) => (
                <tr key={s.id}>
                  <td className="wrap"><b>{s.name}</b><div className="muted small mono">{s.url}{s.version ? ` · ${s.version}` : ''}</div></td>
                  <td className="wrap">
                    <span className={`badge ${s.disabled ? 'b-bad' : s.last_error ? 'b-bad' : s.last_sync ? 'b-ok' : 'b-warn'}`}>{s.disabled ? 'desativado' : s.last_error ? 'erro' : s.last_sync ? 'sincronizado' : 'aguardando'}</span>
                    {!s.disabled && s.last_error && <div className="small" style={{ marginTop: 6 }}>{s.last_error}</div>}
                  </td>
                  <td className="mono" style={{ whiteSpace: 'nowrap' }}>{s.last_sync ? fmtDate(s.last_sync, branding?.timezone) : '—'}</td>
                  <td>{s.messages}</td>
                  <td>
                    <div className="row">
                      <button className="sm" onClick={() => edit(s)}>Editar</button>
                      <button className="sm" onClick={() => run(() => api(`/api/admin/nodes/${s.id}`, { method: 'PATCH', body: { disabled: !s.disabled } }), s.disabled ? 'Servidor ativado' : 'Servidor desativado')}>{s.disabled ? 'Ativar' : 'Desativar'}</button>
                      <button className="sm danger" onClick={async () => (await dlg.confirm({ title: `Remover "${s.name}"?`, message: `As ${s.messages} mensagens registradas dele também serão apagadas. Esta ação não pode ser desfeita.`, danger: true, confirmLabel: 'Remover' })) && run(() => api(`/api/admin/nodes/${s.id}`, { method: 'DELETE' }), 'Servidor removido')}>Remover</button>
                    </div>
                  </td>
                </tr>
              ))}
              {list.length === 0 && <tr><td colSpan={5} className="muted">Nenhum servidor Mailcow cadastrado.</td></tr>}
            </tbody>
          </table>
        </div>
      </div>
    </>
  )
}
