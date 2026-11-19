import { useMemo, useState, type FormEvent } from 'react'
import { api } from '../api'
import { Footer } from '../components/Layout'
import { useBranding } from '../branding'
import Logo from '../components/Logo'
import ThemeToggle from '../components/ThemeToggle'
import { copyText, randomPassword } from '../lib'

const reUser = /^[A-Za-z0-9][A-Za-z0-9._@-]{1,63}$/
const reMail = /^[A-Za-z0-9._%+-]{1,64}@[A-Za-z0-9.-]{1,253}\.[A-Za-z]{2,63}$/

function strength(p: string) {
  let s = 0
  if (p.length >= 10) s++
  if (p.length >= 14) s++
  if (/[a-z]/.test(p) && /[A-Z]/.test(p)) s++
  if (/\d/.test(p)) s++
  if (/[^A-Za-z0-9]/.test(p)) s++
  const lvl = p.length < 10 ? 0 : Math.min(4, Math.max(1, s - 1))
  return { lvl, label: ['Muito curta', 'Fraca', 'Razoável', 'Boa', 'Forte'][lvl] }
}

export default function Setup() {
  const { branding, refresh } = useBranding()
  const needDb = !branding?.db_ready
  const steps = useMemo(() => (needDb ? ['banco', 'acesso', 'senha'] : ['acesso', 'senha']), [needDb])
  const labels: Record<string, string> = { banco: 'Banco de dados', acesso: 'Acesso', senha: 'Senha' }
  const [i, setI] = useState(0)
  const [token, setToken] = useState('')
  const [db, setDb] = useState({ host: 'navalrelay-db', port: '3306', user: 'orbita', password: '', name: 'orbita', sslmode: 'preferred' })
  const [adm, setAdm] = useState({ app_name: 'NavalRelay', username: 'admin', email: '', password: '', confirm: '' })
  const [show, setShow] = useState(false)
  const [gen, setGen] = useState(false)
  const [copied, setCopied] = useState(false)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)
  const hdr = { 'X-Setup-Token': token.trim() }
  const cur = steps[i]

  const emailOk = reMail.test(adm.email.trim())
  const userOk = reUser.test(adm.username.trim())
  const st = strength(adm.password)
  const match = adm.confirm !== '' && adm.password === adm.confirm
  const mismatch = adm.confirm !== '' && adm.password !== adm.confirm

  const run = async (fn: () => Promise<void>) => {
    setBusy(true)
    setErr('')
    try {
      await fn()
    } catch (ex) {
      setErr((ex as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const next = (e: FormEvent) => {
    e.preventDefault()
    if ((cur === 'banco' || (cur === 'acesso' && !needDb)) && !token.trim()) return setErr('Informe o token de configuração (veja os logs do container)')
    if (cur === 'banco') {
      if (!db.host.trim() || !db.user.trim() || !db.name.trim() || !db.port.trim()) return setErr('Preencha host, porta, usuário e banco')
      run(async () => {
        await api('/api/setup/db', { method: 'POST', headers: hdr, body: { ...db, port: Number(db.port) } })
        await refresh()
        setI(i + 1)
      })
    } else if (cur === 'acesso') {
      if (!adm.app_name.trim()) return setErr('Informe o nome do sistema')
      if (!userOk) return setErr('Usuário inválido: use de 2 a 64 caracteres (letras, números, ponto, hífen, sublinhado ou @)')
      if (!emailOk) return setErr('Informe um e-mail válido. Ele é usado na recuperação de senha e no 2FA')
      run(async () => {
        if (!needDb) await api('/api/setup/check', { method: 'POST', headers: hdr })
        setI(i + 1)
      })
    } else {
      if (adm.password.length < 10) return setErr('A senha deve ter ao menos 10 caracteres')
      if (adm.password !== adm.confirm) return setErr('A confirmação da senha não confere')
      run(async () => {
        await api('/api/setup/admin', {
          method: 'POST', headers: hdr,
          body: { app_name: adm.app_name, username: adm.username.trim(), email: adm.email.trim(), password: adm.password, password_confirm: adm.confirm },
        })
        setDone(true)
        await refresh()
        setTimeout(() => window.location.assign('/'), 1400)
      })
    }
  }

  const generate = () => {
    const p = randomPassword(16)
    setAdm({ ...adm, password: p, confirm: p })
    setGen(true)
    setShow(true)
    setErr('')
  }

  const tokenField = (
    <label className="f">Token de configuração
      <input value={token} onChange={(e) => setToken(e.target.value)} required autoFocus autoComplete="off" spellCheck={false} placeholder="cole aqui o token exibido nos logs" />
      <span className="help">Está nos logs do container: <span className="mono">docker compose logs navalrelay-app</span></span>
    </label>
  )

  return (
    <>
      <div className="floating-toggle"><ThemeToggle /></div>
      <div className="center">
        <form className="auth glass setup" style={{ maxWidth: 560 }} onSubmit={next} noValidate>
          <div className="logo"><Logo size={72} /></div>
          <h1>Bem-vindo ao {adm.app_name || 'NavalRelay'}</h1>
          <p className="muted small" style={{ textAlign: 'center', marginTop: -6 }}>
            {done ? 'Tudo pronto!' : `Configuração inicial · etapa ${i + 1} de ${steps.length}`}
          </p>
          <div className="wiz-steps" style={{ justifyContent: 'center' }}>
            {steps.map((s, k) => (
              <span key={s} className={`st ${k === i && !done ? 'on' : k < i || done ? 'done' : ''}`}>{k < i || done ? '✓' : k + 1} {labels[s]}</span>
            ))}
          </div>

          {done ? (
            <div className="stack" style={{ alignItems: 'center', textAlign: 'center', padding: '10px 0' }}>
              <div className="dlg-icon" style={{ width: 56, height: 56, fontSize: '1.6rem', borderColor: 'var(--success)', color: 'var(--success)' }}>✓</div>
              <b>Administrador criado com sucesso</b>
              <span className="muted small">Abrindo o assistente de configuração…</span>
            </div>
          ) : (
            <div className="stack setup-step" key={cur}>
              {cur === 'banco' && (
                <>
                  <p className="muted small">Informe o token de configuração e os dados do MySQL. Eles são testados antes de seguir.</p>
                  {tokenField}
                  <div className="form-grid">
                    <label className="f">Host<input value={db.host} onChange={(e) => setDb({ ...db, host: e.target.value })} required /></label>
                    <label className="f">Porta<input value={db.port} onChange={(e) => setDb({ ...db, port: e.target.value })} required inputMode="numeric" /></label>
                    <label className="f">Usuário<input value={db.user} onChange={(e) => setDb({ ...db, user: e.target.value })} required /></label>
                    <label className="f">Senha<input type="password" value={db.password} onChange={(e) => setDb({ ...db, password: e.target.value })} autoComplete="off" /></label>
                    <label className="f">Banco<input value={db.name} onChange={(e) => setDb({ ...db, name: e.target.value })} required /></label>
                    <label className="f">SSL
                      <select value={db.sslmode} onChange={(e) => setDb({ ...db, sslmode: e.target.value })}>
                        {['disable', 'preferred', 'required', 'skip-verify'].map((m) => <option key={m}>{m}</option>)}
                      </select>
                    </label>
                  </div>
                </>
              )}

              {cur === 'acesso' && (
                <>
                  <p className="muted small">{needDb ? 'Agora os dados do administrador do sistema.' : 'Informe o token de configuração e os dados do administrador do sistema.'}</p>
                  {!needDb && tokenField}
                  <label className="f">Nome do sistema<input value={adm.app_name} onChange={(e) => setAdm({ ...adm, app_name: e.target.value })} required maxLength={60} autoFocus={needDb} /></label>
                  <div className="form-grid">
                    <label className="f">Usuário<input value={adm.username} onChange={(e) => setAdm({ ...adm, username: e.target.value })} required autoComplete="username" aria-invalid={adm.username !== '' && !userOk} /></label>
                    <label className="f">E-mail<input type="email" value={adm.email} onChange={(e) => setAdm({ ...adm, email: e.target.value })} required autoComplete="email" placeholder="voce@empresa.com.br" aria-invalid={adm.email !== '' && !emailOk} />
                      <span className="help">Usado na recuperação de senha e no 2FA</span>
                    </label>
                  </div>
                </>
              )}

              {cur === 'senha' && (
                <>
                  <p className="muted small">Defina a senha de <b>{adm.username}</b>. Use ao menos 10 caracteres.</p>
                  <label className="f">Senha
                    <div className="row" style={{ gap: 8, flexWrap: 'nowrap' }}>
                      <input style={{ flex: 1 }} type={show ? 'text' : 'password'} value={adm.password} onChange={(e) => { setGen(false); setAdm({ ...adm, password: e.target.value }) }} minLength={10} maxLength={72} required autoComplete="new-password" autoFocus />
                      <button type="button" className="sm" onClick={() => setShow(!show)}>{show ? 'Ocultar' : 'Mostrar'}</button>
                    </div>
                  </label>
                  <div className="meter" aria-label={`Força da senha: ${st.label}`}>
                    {[1, 2, 3, 4].map((n) => <span key={n} className={st.lvl >= n ? `on l${st.lvl}` : ''} />)}
                    <em>{adm.password ? st.label : ''}</em>
                  </div>
                  <label className="f">Confirme a senha
                    <input type={show ? 'text' : 'password'} value={adm.confirm} onChange={(e) => setAdm({ ...adm, confirm: e.target.value })} minLength={10} maxLength={72} required autoComplete="new-password" aria-invalid={mismatch} />
                    {mismatch && <span className="help" style={{ color: 'var(--danger)' }}>As senhas não conferem</span>}
                    {match && <span className="help" style={{ color: 'var(--success)' }}>As senhas conferem ✓</span>}
                  </label>
                  <div className="row">
                    <button type="button" className="sm" onClick={generate}>⚄ Gerar senha forte</button>
                    {gen && <button type="button" className="sm" onClick={async () => { setCopied(await copyText(adm.password)); setTimeout(() => setCopied(false), 1800) }}>{copied ? 'Copiado ✓' : '⧉ Copiar senha'}</button>}
                    {gen && <span className="help">Guarde a senha antes de concluir.</span>}
                  </div>
                </>
              )}

              {err && <div className="alert err" role="alert">{err}</div>}
              <div className="row spread" style={{ marginTop: 4 }}>
                <button type="button" disabled={i === 0 || busy} onClick={() => { setErr(''); setI(i - 1) }}>← Voltar</button>
                <button className="primary" disabled={busy}>
                  {busy ? 'Aguarde…' : cur === 'senha' ? 'Concluir ✓' : cur === 'banco' ? 'Testar e avançar →' : 'Avançar →'}
                </button>
              </div>
            </div>
          )}
        </form>
      </div>
      <div className="footer-fixed"><Footer /></div>
    </>
  )
}
