import { useId } from 'react'

export default function Logo({ size = 40 }: { size?: number }) {
  const id = useId().replace(/:/g, '')
  return (
    <svg className="logo-mark" width={size} height={size} viewBox="0 0 64 64" role="img" aria-label="NavalRelay">
      <defs>
        <linearGradient id={`g${id}`} gradientUnits="userSpaceOnUse" x1="10" y1="4" x2="54" y2="60">
          <stop offset="0" stopColor="var(--primary)" />
          <stop offset="1" style={{ stopColor: 'color-mix(in srgb, var(--primary) 60%, #000)' }} />
        </linearGradient>
        <filter id={`f${id}`} x="-40%" y="-40%" width="180%" height="180%">
          <feGaussianBlur stdDeviation="1.6" result="b" />
          <feMerge><feMergeNode in="b" /><feMergeNode in="SourceGraphic" /></feMerge>
        </filter>
      </defs>
      <g filter={`url(#f${id})`} fill="none" strokeLinecap="round" strokeLinejoin="round">
        <path d="M32 6C40 6 46 3.5 52 6.5L54 9V31C54 45 45 53.5 32 59C19 53.5 10 45 10 31V9L12 6.5C18 3.5 24 6 32 6Z" stroke={`url(#g${id})`} strokeWidth="2.6" style={{ fill: 'color-mix(in srgb, var(--primary) 8%, transparent)' }} />
        <path d="M32 10.5C39 10.5 44 8.5 49 10.5V31C49 42.5 41.5 49.5 32 54C22.5 49.5 15 42.5 15 31V10.5C20 8.5 25 10.5 32 10.5Z" stroke={`url(#g${id})`} strokeWidth="1" opacity="0.55" />
        <g stroke={`url(#g${id})`}>
          <circle cx="32" cy="17.5" r="3" strokeWidth="2.2" />
          <path d="M25 24.5H39" strokeWidth="2.6" />
          <path d="M32 20.5V48" strokeWidth="2.8" />
          <path d="M21 37C22 44 26.5 48 32 48.5C37.5 48 42 44 43 37" strokeWidth="2.6" />
        </g>
        <path d="M17.8 39.4L20.4 33L24.2 37.8ZM46.2 39.4L43.6 33L39.8 37.8Z" fill="var(--primary)" stroke="var(--primary)" strokeWidth="1" />
        <path d="M40.5 14.5Q43.2 17.5 40.5 20.5M43.8 12Q48.6 17.5 43.8 23" stroke="var(--secondary)" strokeWidth="1.8" />
      </g>
    </svg>
  )
}
