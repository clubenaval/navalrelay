import { useCallback, useEffect, useRef, useState } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, roleLabel, type User } from '../api'
import { useBranding } from '../branding'
import Icon from './Icons'
import Logo from './Logo'
import { currentMode, toggleMode } from './ThemeToggle'

export function Footer() {
  const { branding } = useBranding()
  return (
    <footer className="footer">
      <div className="line">
        <b>{branding?.app_name ?? 'NavalRelay'} v{__APP_VERSION__}</b> - Relay SMTP e relatórios - Desenvolvido por{' '}
        <a href="https://clubenaval.org.br" target="_blank" rel="noopener noreferrer">Assessoria de TI</a>
      </div>
    </footer>
  )
}

export function Brand({ size = 40 }: { size?: number }) {
  const { branding } = useBranding()
  return (
    <div className="brand">
      <Logo size={size} />
      <span>{branding?.app_name ?? 'NavalRelay'}</span>
    </div>
  )
}

const initials = (u: User) => {
  const n = u.username.trim()
  const parts = n.split(/[\s._@-]+/).filter(Boolean)
  return ((parts[0]?.[0] ?? '?') + (parts.length > 1 ? parts[1][0] : parts[0]?.[1] ?? '')).toUpperCase()
}

function Item({ to, icon, label, end, badge, dot }: { to: string; icon: string; label: string; end?: boolean; badge?: number; dot?: 'ok' | 'err' }) {
  return (
    <NavLink to={to} end={end} title={label}>
      <Icon name={icon} />
      <span className="lbl">{label}</span>
      {badge ? <span className="cnt" title={`${badge} com falha hoje`}>{badge > 99 ? '99+' : badge}</span> : null}
      {dot && <span className={`dot ${dot}`} title={dot === 'err' ? 'Há servidor com erro' : 'Sincronizando normalmente'} />}
    </NavLink>
  )
}

export default function Layout({ user }: { user: User }) {
  const nav = useNavigate()
  const loc = useLocation()
  const { branding } = useBranding()
  const admin = user.role === 'admin'
  const smarthost = branding?.relay_mode === 'smarthost'
  const central = branding?.deploy_mode === 'server'

  const [collapsed, setCollapsed] = useState(() => { try { return localStorage.getItem('orbita.side') === '1' } catch { return false } })
  const [open, setOpen] = useState(false)
  const [menu, setMenu] = useState(false)
  const [mode, setMode] = useState(currentMode())
  const [failed, setFailed] = useState(0)
  const [mc, setMc] = useState<'ok' | 'err' | undefined>()
  const cardRef = useRef<HTMLDivElement>(null)

  const toggleCollapsed = () => {
    const v = !collapsed
    setCollapsed(v)
    try { localStorage.setItem('orbita.side', v ? '1' : '0') } catch { /* ignora */ }
  }

  useEffect(() => { setOpen(false); setMenu(false) }, [loc.pathname])
  useEffect(() => {
    const on = (e: Event) => setMode((e as CustomEvent<'dark' | 'light'>).detail)
    window.addEventListener('orbita-mode', on)
    return () => window.removeEventListener('orbita-mode', on)
  }, [])
  useEffect(() => {
    if (!menu) return
    const click = (e: MouseEvent) => { if (cardRef.current && !cardRef.current.contains(e.target as Node)) setMenu(false) }
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') setMenu(false) }
    document.addEventListener('mousedown', click)
    document.addEventListener('keydown', key)
    return () => { document.removeEventListener('mousedown', click); document.removeEventListener('keydown', key) }
  }, [menu])

  const poll = useCallback(async () => {
    if (document.hidden) return
    try {
      const s = await api<{ totals: Record<string, number> }>('/api/stats?days=1')
      setFailed(s.totals.failed ?? 0)
    } catch { /* ignora */ }
    if (admin) {
      try {
        const l = await api<{ disabled: boolean; last_error: string; last_sync: string | null }[]>('/api/admin/mailcow')
        const act = l.filter((x) => !x.disabled)
        setMc(act.length === 0 ? undefined : act.some((x) => x.last_error) ? 'err' : 'ok')
      } catch { /* ignora */ }
    }
  }, [admin])
  useEffect(() => {
    poll()
    const t = setInterval(poll, 60000)
    return () => clearInterval(t)
  }, [poll])

  const logout = async () => {
    await api('/api/auth/logout', { method: 'POST' }).catch(() => {})
    nav(0)
  }

  return (
    <>
      <div className="shell">
        <header className="mobilebar glass">
          <button className="sm ghost" onClick={() => setOpen(true)} aria-label="Abrir menu"><Icon name="menu" size={20} /></button>
          <Brand size={30} />
        </header>
        {open && <div className="side-overlay" onClick={() => setOpen(false)} />}
        <aside className={`side glass ${collapsed ? 'collapsed' : ''} ${open ? 'open' : ''}`}>
          <div className="side-head">
            <Brand size={collapsed ? 34 : 40} />
            <button className="sm ghost collapse" onClick={toggleCollapsed} title={collapsed ? 'Expandir menu' : 'Recolher menu'} aria-label={collapsed ? 'Expandir menu' : 'Recolher menu'}>
              <Icon name={collapsed ? 'right' : 'left'} size={16} />
            </button>
            <button className="sm ghost closer" onClick={() => setOpen(false)} aria-label="Fechar menu"><Icon name="close" size={18} /></button>
          </div>
          <nav className="nav" aria-label="Principal">
            <Item to="/" end icon="dashboard" label="Visão geral" />
            <Item to="/mensagens" icon="mail" label="Mensagens" badge={failed} />
            {admin && (
              <>
                <div className="sep">Fontes</div>
                {central && <Item to="/admin/servidores" icon="relay" label="Relays remotos" />}
                <Item to="/admin/mailcow" icon="inbox" label="Mailcow" dot={mc} />
                <div className="sep">Sistema</div>
                {!smarthost && <Item to="/dominios" icon="globe" label="Domínios e DKIM" />}
                <Item to="/admin/usuarios" icon="users" label="Usuários" />
                <Item to="/admin/auditoria" icon="audit" label="Auditoria" />
                <Item to="/admin/configuracoes" icon="settings" label="Configurações" />
              </>
            )}
          </nav>
          <div className="grow" />
          <div className="usercard-wrap" ref={cardRef}>
            {menu && (
              <div className="usermenu glass" role="menu">
                {user.source === 'local' && (
                  <NavLink to="/conta" role="menuitem"><Icon name="user" /> Minha conta</NavLink>
                )}
                <NavLink to="/seguranca" role="menuitem"><Icon name="shield" /> Segurança (2FA)</NavLink>
                <button role="menuitem" onClick={() => setMode(toggleMode())}><Icon name={mode === 'dark' ? 'sun' : 'moon'} /> {mode === 'dark' ? 'Modo claro' : 'Modo escuro'}</button>
                <button role="menuitem" className="out" onClick={logout}><Icon name="logout" /> Sair</button>
              </div>
            )}
            <button className="usercard" onClick={() => setMenu(!menu)} aria-haspopup="menu" aria-expanded={menu} title={`${user.username} · ${roleLabel[user.role]}`}>
              <span className="avatar">{initials(user)}</span>
              <span className="who">
                <b>{user.username}</b>
                <small>{roleLabel[user.role]}</small>
              </span>
              <span className="chev"><Icon name="chevup" size={14} /></span>
            </button>
          </div>
        </aside>
        <div className="main">
          <main className="content">
            <Outlet />
          </main>
          <Footer />
        </div>
      </div>
    </>
  )
}
