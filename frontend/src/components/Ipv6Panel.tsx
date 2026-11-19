import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'

type Status = {
  level: 'ok' | 'partial' | 'off'
  issues: string[]
  docker: { available: boolean; error?: string; version?: string; major?: number; api_version?: string; experimental: boolean; daemon_ipv6: boolean | null }
  container: { ipv6: boolean; addresses: string[]; gateways: string[] }
  outbound6: { ok: boolean; detail: string }
  masked: { count: number; last: string }
}

const badge = { ok: ['b-ok', 'IPv6 pronto'], partial: ['b-warn', 'IPv6 parcial'], off: ['b-bad', 'IPv6 indisponível'] } as const

async function copyText(t: string) {
  try {
    await navigator.clipboard.writeText(t)
  } catch {
    const ta = document.createElement('textarea')
    ta.value = t
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    ta.remove()
  }
}

function Snippet({ text }: { text: string }) {
  const [done, setDone] = useState(false)
  return (
    <div className="stack" style={{ gap: 6 }}>
      <code className="block">{text}</code>
      <div><button type="button" className="sm" onClick={async () => { await copyText(text); setDone(true); setTimeout(() => setDone(false), 1500) }}>{done ? 'Copiado ✓' : 'Copiar'}</button></div>
    </div>
  )
}

const yn = (v: boolean | null | undefined) => (v == null ? 'não detectado' : v ? 'sim' : 'não')

export default function Ipv6Panel() {
  const [st, setSt] = useState<Status | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [manual, setManual] = useState<'new' | 'old'>('new')

  const load = useCallback(async () => {
    setBusy(true)
    setErr('')
    try {
      setSt(await api<Status>('/api/admin/ipv6'))
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }, [])
  useEffect(() => { load() }, [load])

  if (!st) return <div className="glass card muted small">{err || 'Verificando IPv6 no Docker…'}</div>

  const known = st.docker.available && !!st.docker.major
  const isNew = known ? (st.docker.major ?? 0) >= 27 : manual === 'new'
  const json = isNew
    ? '{\n  "ipv6": true,\n  "ip6tables": true\n}'
    : '{\n  "ipv6": true,\n  "ip6tables": true,\n  "experimental": true\n}'
  const [cls, label] = badge[st.level]

  return (
    <div className="glass card stack ipv6">
      <div className="row spread">
        <b>IPv6 no Docker</b>
        <span className={`badge ${cls}`}>{label}</span>
      </div>
      <div className="kv">
        <span className="muted">Versão do Docker</span>
        <span>{known ? `${st.docker.version} (API ${st.docker.api_version})` : 'não detectada'}</span>
        <span className="muted">IPv6 no daemon</span><span>{yn(st.docker.daemon_ipv6)}</span>
        <span className="muted">IPv6 no container</span>
        <span>{st.container.ipv6 ? <>sim <span className="mono">{(st.container.addresses ?? [])[0]}</span></> : 'não'}</span>
        <span className="muted">Saída IPv6 à internet</span><span>{st.outbound6.ok ? 'sim' : 'não'}</span>
        <span className="muted">Origem mascarada</span>
        <span>{st.masked.count > 0 ? `sim (${st.masked.count}${st.masked.last ? `, última em ${st.masked.last}` : ''})` : 'não'}</span>
      </div>
      {st.level === 'ok' && <div className="alert ok">O Docker está preparado para IPv6: o container tem endereço IPv6, a saída funciona e o IP de origem das conexões é preservado.</div>}
      {(st.issues ?? []).map((i) => <div className="alert warn" key={i}>{i}</div>)}

      {st.level !== 'ok' && (
        <div className="tip" role="note">
          <b className="tip-title">Como habilitar o IPv6 no Docker{known ? ` ${st.docker.version}` : ''}</b>
          {!known && (
            <label className="f" style={{ maxWidth: 360, margin: '4px 0 8px' }}>Versão do Docker (docker --version)
              <select value={manual} onChange={(e) => setManual(e.target.value as 'new' | 'old')}>
                <option value="new">27 ou mais nova</option>
                <option value="old">26 ou mais antiga</option>
              </select>
              <span className="help">{st.docker.error ? `Não foi possível detectar automaticamente (${st.docker.error}). ` : ''}Mantenha o serviço navalrelay-proxy do compose para detectar a versão sozinho.</span>
            </label>
          )}
          <ol>
            <li>No servidor que roda o Docker, edite <span className="mono">/etc/docker/daemon.json</span> (crie o arquivo se não existir). Se já houver outras opções, apenas acrescente estas chaves:
              <Snippet text={json} />
              {!isNew && <span className="help">Em versões anteriores à 27, o <span className="mono">ip6tables</span> exige o modo experimental.</span>}
            </li>
            <li>Reinicie o Docker. Isso reinicia todos os containers da máquina:
              <Snippet text="sudo systemctl restart docker" />
            </li>
            <li>Recrie a stack do NavalRelay, na pasta do docker-compose.yml:
              <Snippet text="docker compose up -d --force-recreate" />
            </li>
            <li>Volte aqui e clique em <b>Verificar novamente</b>.</li>
          </ol>
          <span className="help">O servidor também precisa ter um endereço IPv6 global e o encaminhamento ativo (<span className="mono">net.ipv6.conf.all.forwarding=1</span>). Sem IPv6 no Docker, o relay continua funcionando normalmente em IPv4.</span>
        </div>
      )}
      {err && <div className="alert err">{err}</div>}
      <div><button type="button" onClick={load} disabled={busy}>{busy ? 'Verificando…' : 'Verificar novamente'}</button></div>
    </div>
  )
}
