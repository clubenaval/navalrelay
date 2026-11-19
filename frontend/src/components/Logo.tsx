export default function Logo({ size = 40 }: { size?: number }) {
  return <img className="logo-mark" src="/logo.png" height={size} width={Math.round(size * 0.904)} alt="NavalRelay" />
}
