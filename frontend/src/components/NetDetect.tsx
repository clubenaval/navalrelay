import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api'

type Item = { cidr: string; label: string; source: string }
type Resp = { auto: boolean; items: Item[]; suggestions: Item[]; effective: string[] }

export default function NetDetect({ auto, version, current, onAdd, autoAdd = false }: { auto: boolean; version: number; current: string; onAdd: (list: string[]) => void; autoAdd?: boolean }) {
  const did = useRef(false)
  const inField = new Set(current.split('\n').map((l) => l.trim()).filter(Boolean))
  const [d, setD] = useState<Resp | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [manual, setManual] = useState('')
  const [added, setAdded] = useState('')

  const run = useCallback(async () => {
    setBusy(true)
    setErr('')
    try {
      setD(await api<Resp>('/api/admin/networks/detect', { method: 'POST' }))
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }, [])
  useEffect(() => { run() }, [run, version])

  const pend = (d?.suggestions ?? []).filter((i) => !inField.has(i.cidr))

  useEffect(() => {
    if (!autoAdd || did.current || !d) return
    did.current = true
    const all = [...(d.items ?? []), ...(d.suggestions ?? [])].map((i) => i.cidr).filter((c) => !inField.has(c))
    if (all.length) {
      onAdd(all)
      setAdded(`${all.length} rede(s) encontrada(s) já adicionada(s) à lista. Remova as que não quiser`)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [d, autoAdd])

  const add = (list: string[], msg: string) => {
    onAdd(list)
    setAdded(msg)
    setTimeout(() => setAdded(''), 2500)
  }
  const addManual = () => {
    const list = manual.split(/[\s,;]+/).filter(Boolean)
    if (list.length) {
      add(list, `${list.length} item(ns) adicionado(s) ao campo acima`)
      setManual('')
    }
  }

  const pendDet = (d?.items ?? []).filter((i) => !inField.has(i.cidr))
  const all = [...pendDet, ...pend]
  const extra = auto && d?.auto ? pendDet.length : 0

  return (
    <div className="glass card stack" style={{ background: 'transparent' }}>
      <div className="row spread">
        <b>Redes encontradas (máquina e Docker)</b>
        <div className="row">
          {added && <span className="muted small">{added} ✓</span>}
          <button type="button" className="sm" onClick={run} disabled={busy}>{busy ? 'Detectando…' : '↻ Detectar novamente'}</button>
        </div>
      </div>
      {err && <div className="alert err">{err}</div>}
      {d && (
        <>
          <div className="alert info small">
            <b>Quem pode enviar agora:</b> {inField.size} {inField.size === 1 ? 'rede' : 'redes'} na lista
            {extra > 0 && <> + {extra} detectada(s) automaticamente</>}.
            {!(auto && d.auto) && <> A inclusão automática está desligada: só vale o que está na lista.</>}
          </div>

          {all.length > 0 ? (
            <details open={pendDet.length > 0}>
              <summary style={{ cursor: 'pointer' }}><b>Outros IPs e redes encontrados ({all.length})</b> <span className="muted small">— confira antes de adicionar</span></summary>
              <div className="row" style={{ margin: '10px 0' }}>
                <button type="button" className="sm" onClick={() => add(all.map((i) => i.cidr), `${all.length} item(ns) adicionado(s) à lista acima`)}>＋ Adicionar todos ({all.length})</button>
              </div>
              <div className="tablewrap">
                <table>
                  <tbody>
                    {all.map((i) => (
                      <tr key={i.cidr}>
                        <td className="mono">{i.cidr}</td>
                        <td className="wrap">{i.label}</td>
                        <td><button type="button" className="sm" onClick={() => add([i.cidr], `${i.cidr} adicionado à lista acima`)}>Adicionar</button></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </details>
          ) : (
            <span className="muted small">✓ Todas as redes encontradas já estão na lista acima.</span>
          )}

          <details>
            <summary style={{ cursor: 'pointer' }}><b>Adicionar IPs da máquina (host)</b> <span className="muted small">— de dentro do contêiner não dá para lê-los</span></summary>
            <div className="stack" style={{ gap: 6, marginTop: 8 }}>
              <span className="help">Rode <span className="mono">hostname -I</span> no servidor e cole o resultado (vários IPv4 e IPv6, separados por espaço). Também dá para fixá-los em <span className="mono">ORBITA_HOST_IPS</span> no serviço <span className="mono">navalrelay-app</span>.</span>
              <div className="row">
                <input style={{ flex: '1 1 320px' }} value={manual} onChange={(e) => setManual(e.target.value)} placeholder="192.168.0.10 2804:14c:... 172.18.0.1" aria-label="IPs da máquina" />
                <button type="button" className="sm" onClick={addManual} disabled={!manual.trim()}>Adicionar</button>
              </div>
            </div>
          </details>
          <span className="help">Conexões vindas pelo gateway do Docker continuam bloqueadas por segurança, mesmo dentro de uma rede autorizada.</span>
        </>
      )}
    </div>
  )
}
