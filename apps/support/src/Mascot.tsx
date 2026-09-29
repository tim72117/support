import styles from './Mascot.module.css'

export type MascotId = 'fox' | 'bear' | 'cat' | 'bird'

interface MascotProps {
  id: MascotId
  color: string
  size?: number
  /** Plays a small idle bounce/blink loop — used on the welcome screen and the chat header. */
  animated?: boolean
}

// Hand-drawn-style SVG mascot, no external art assets (matches
// apps/console/src/Mascot.tsx's character set so a business's console
// preview and its real page show the same character). Kept as its own
// copy rather than a shared package since these are two independent apps
// in this scaffold.
export function Mascot({ id, color, size = 96, animated = false }: MascotProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 96 96"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      role="img"
      aria-label="服務吉祥物"
      className={animated ? styles.bounce : undefined}
    >
      <MascotEars id={id} color={color} />
      <ellipse cx="48" cy="54" rx="30" ry="28" fill={color} />
      <ellipse cx="26" cy="60" rx="6" ry="4" fill="white" opacity="0.35" />
      <ellipse cx="70" cy="60" rx="6" ry="4" fill="white" opacity="0.35" />
      <g className={animated ? styles.blink : undefined}>
        <circle cx="38" cy="50" r="3.4" fill="#2c2a28" />
        <circle cx="58" cy="50" r="3.4" fill="#2c2a28" />
      </g>
      <path
        d="M40 62c2.8 3 13.2 3 16 0"
        stroke="#2c2a28"
        strokeWidth="3"
        strokeLinecap="round"
        fill="none"
      />
      <MascotSnoutDetail id={id} />
    </svg>
  )
}

function MascotEars({ id, color }: { id: MascotId; color: string }) {
  switch (id) {
    case 'fox':
      return (
        <>
          <path d="M24 20 L34 44 L14 40 Z" fill={color} />
          <path d="M72 20 L82 40 L62 44 Z" fill={color} />
        </>
      )
    case 'bear':
      return (
        <>
          <circle cx="22" cy="26" r="11" fill={color} />
          <circle cx="74" cy="26" r="11" fill={color} />
        </>
      )
    case 'cat':
      return (
        <>
          <path d="M20 30 L28 8 L38 32 Z" fill={color} />
          <path d="M76 30 L68 8 L58 32 Z" fill={color} />
        </>
      )
    case 'bird':
      return <ellipse cx="48" cy="20" rx="10" ry="8" fill={color} />
  }
}

function MascotSnoutDetail({ id }: { id: MascotId }) {
  if (id === 'bird') {
    return <path d="M42 56 L48 62 L54 56 Z" fill="#ffb84c" />
  }
  return null
}
