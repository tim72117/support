import type { MascotId } from './model.ts'

interface MascotProps {
  id: MascotId
  color: string
  size?: number
}

// Simple hand-drawn-style SVG mascots, no external art assets. Each is a
// rounded blob body + face so they read as "the same character set", but
// every species gets a *large* silhouette feature (ears / crest / floppy
// flaps / flippers) plus one fixed-colour marking (black ear tips, pink
// snout, orange beak, goggles, eye patch…) so the ten stay tellable apart
// even as a 32–40px picker thumbnail. `color` re-tints the body (and the
// theme-coloured parts) per business (see model.THEME_COLORS) — the
// markings use fixed ink/pink/orange so they stay legible against any
// theme colour, including the white body used on the consumer hero.
//
// Keep this file visually in sync with apps/support/src/Mascot.tsx.
export function Mascot({ id, color, size = 96 }: MascotProps) {
  const face = FACE[id]
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 96 96"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      role="img"
      aria-label="吉祥物"
    >
      <MascotBack id={id} color={color} />
      <MascotBody id={id} color={color} />
      {/* cheeks */}
      <ellipse cx={48 - face.eyeDx - 12} cy={face.eyeY + 10} rx="6" ry="4" fill="white" opacity="0.35" />
      <ellipse cx={48 + face.eyeDx + 12} cy={face.eyeY + 10} rx="6" ry="4" fill="white" opacity="0.35" />
      <MascotMarkings id={id} />
      {/* eyes */}
      <circle cx={48 - face.eyeDx} cy={face.eyeY} r={face.eyeR} fill={INK} />
      <circle cx={48 + face.eyeDx} cy={face.eyeY} r={face.eyeR} fill={INK} />
      {face.mouth && <path d={face.mouth} stroke={INK} strokeWidth="3" strokeLinecap="round" fill="none" />}
      <MascotFront id={id} />
    </svg>
  )
}

const INK = '#2c2a28'
const PINK = '#f4a7b9'
const TONGUE = '#f06a7e'
// Deliberately not the same hue as the '#FFB84C' theme colour, and always
// drawn with an ink outline, so beaks/feet never dissolve into the body.
const BEAK = '#f79a2a'

interface Face {
  /** Vertical position of the eyes (the body is lower on some species). */
  eyeY: number
  /** Half the distance between the eyes. */
  eyeDx: number
  eyeR: number
  /** Mouth path, or null when a beak/snout replaces it. */
  mouth: string | null
}

const FACE: Record<MascotId, Face> = {
  fox: { eyeY: 50, eyeDx: 10, eyeR: 3.4, mouth: 'M41 64c2.5 3 11.5 3 14 0' },
  bear: { eyeY: 50, eyeDx: 10, eyeR: 3.4, mouth: 'M42 66c2.5 3 9.5 3 12 0' },
  // "ω" cat mouth under a pink nose.
  cat: { eyeY: 50, eyeDx: 10, eyeR: 3.4, mouth: 'M41 61q3.5 4.5 7 0q3.5 4.5 7 0' },
  bird: { eyeY: 48, eyeDx: 10, eyeR: 3.4, mouth: null },
  rabbit: { eyeY: 54, eyeDx: 10, eyeR: 3.4, mouth: 'M42 63c2 3 10 3 12 0' },
  dog: { eyeY: 50, eyeDx: 10, eyeR: 3.4, mouth: 'M41 63c2.5 4 11.5 4 14 0' },
  owl: { eyeY: 50, eyeDx: 10, eyeR: 4.6, mouth: null },
  penguin: { eyeY: 50, eyeDx: 10, eyeR: 3.4, mouth: null },
  panda: { eyeY: 50, eyeDx: 10, eyeR: 2.6, mouth: 'M42 65c2.5 3 9.5 3 12 0' },
  pig: { eyeY: 49, eyeDx: 12, eyeR: 3.4, mouth: 'M43 73c2 2 8 2 10 0' },
}

/** The body blob. Most species share the same ellipse; a few change the
 * proportions so the silhouette itself differs (egg-shaped bird, lower
 * rabbit body to make room for the ears, wider pig/bear). */
function MascotBody({ id, color }: { id: MascotId; color: string }) {
  switch (id) {
    case 'bird':
      return <path d="M48 24C62 24 74 40 74 58C74 74 62 84 48 84C34 84 22 74 22 58C22 40 34 24 48 24Z" fill={color} />
    case 'rabbit':
      return <ellipse cx="48" cy="58" rx="28" ry="26" fill={color} />
    case 'bear':
    case 'panda':
      return <ellipse cx="48" cy="54" rx="32" ry="28" fill={color} />
    case 'pig':
      return <ellipse cx="48" cy="55" rx="32" ry="27" fill={color} />
    default:
      return <ellipse cx="48" cy="54" rx="30" ry="28" fill={color} />
  }
}

/** Shapes drawn *behind* the body: ears, crests, tails, feet. These set
 * the silhouette, so they are sized to be large relative to the body. */
function MascotBack({ id, color }: { id: MascotId; color: string }) {
  switch (id) {
    case 'fox':
      // Tall, wide-set triangular ears with black tips, plus a bushy tail
      // peeking out bottom-right.
      return (
        <>
          <ellipse cx="79" cy="74" rx="12" ry="9" fill={color} transform="rotate(-35 79 74)" />
          <circle cx="86" cy="69" r="4.5" fill="white" opacity="0.6" />
          <path d="M14 42L24 2L46 28Z" fill={color} />
          <path d="M82 42L72 2L50 28Z" fill={color} />
          <path d="M24 2L20 18L33 12.5Z" fill={INK} />
          <path d="M72 2L76 18L63 12.5Z" fill={INK} />
        </>
      )
    case 'bear':
      return (
        <>
          <circle cx="20" cy="28" r="12" fill={color} />
          <circle cx="76" cy="28" r="12" fill={color} />
          <circle cx="20" cy="28" r="6" fill={INK} opacity="0.25" />
          <circle cx="76" cy="28" r="6" fill={INK} opacity="0.25" />
        </>
      )
    case 'cat':
      // Pointed ears with pink inner ears.
      return (
        <>
          <path d="M18 36L26 6L42 28Z" fill={color} />
          <path d="M78 36L70 6L54 28Z" fill={color} />
          <path d="M22.8 30.3L27.2 13.8L36 25.9Z" fill={PINK} />
          <path d="M73.2 30.3L68.8 13.8L60 25.9Z" fill={PINK} />
        </>
      )
    case 'bird':
      // Three-feather crest and little orange feet.
      return (
        <>
          <ellipse cx="48" cy="16" rx="4.5" ry="13" fill={color} />
          <ellipse cx="39" cy="20" rx="4.5" ry="12" fill={color} transform="rotate(-32 39 20)" />
          <ellipse cx="57" cy="20" rx="4.5" ry="12" fill={color} transform="rotate(32 57 20)" />
          <ellipse cx="40" cy="85" rx="6" ry="3.2" fill={BEAK} stroke={INK} strokeWidth="1.2" />
          <ellipse cx="56" cy="85" rx="6" ry="3.2" fill={BEAK} stroke={INK} strokeWidth="1.2" />
        </>
      )
    case 'rabbit':
      // Very long upright ears (a third of the whole figure) with pink
      // inner ears.
      return (
        <>
          <ellipse cx="37" cy="17" rx="7.5" ry="17" fill={color} transform="rotate(-6 37 17)" />
          <ellipse cx="59" cy="17" rx="7.5" ry="17" fill={color} transform="rotate(6 59 17)" />
          <ellipse cx="37" cy="18" rx="3.5" ry="12" fill={PINK} transform="rotate(-6 37 18)" />
          <ellipse cx="59" cy="18" rx="3.5" ry="12" fill={PINK} transform="rotate(6 59 18)" />
        </>
      )
    case 'dog':
      // Big floppy ears hanging down the sides of the body, shaded darker
      // than the body.
      return (
        <>
          <ellipse cx="17" cy="48" rx="11" ry="20" fill={color} transform="rotate(-10 17 48)" />
          <ellipse cx="79" cy="48" rx="11" ry="20" fill={color} transform="rotate(10 79 48)" />
          <ellipse cx="17" cy="48" rx="11" ry="20" fill={INK} opacity="0.3" transform="rotate(-10 17 48)" />
          <ellipse cx="79" cy="48" rx="11" ry="20" fill={INK} opacity="0.3" transform="rotate(10 79 48)" />
        </>
      )
    case 'owl':
      // Horn-like ear tufts pointing up and outward.
      return (
        <>
          <path d="M16 36L15 12L38 28Z" fill={color} />
          <path d="M80 36L81 12L58 28Z" fill={color} />
        </>
      )
    case 'penguin':
      return (
        <>
          <ellipse cx="39" cy="84" rx="7" ry="3.5" fill={BEAK} stroke={INK} strokeWidth="1.2" />
          <ellipse cx="57" cy="84" rx="7" ry="3.5" fill={BEAK} stroke={INK} strokeWidth="1.2" />
        </>
      )
    case 'panda':
      // Always-black round ears.
      return (
        <>
          <circle cx="20" cy="28" r="12" fill={INK} />
          <circle cx="76" cy="28" r="12" fill={INK} />
        </>
      )
    case 'pig':
      // Small folded pointy ears with pink insides, and a curly tail.
      return (
        <>
          <path d="M18 40L22 14L40 30Z" fill={color} />
          <path d="M78 40L74 14L56 30Z" fill={color} />
          <path d="M22.3 36L24.1 20.5L34.7 29.4Z" fill={PINK} />
          <path d="M73.7 36L71.9 20.5L61.3 29.4Z" fill={PINK} />
          <path
            d="M78 64c7-5 12 1 7 5c-3 2.5-6 0-4-2.5"
            stroke={INK}
            strokeWidth="2.5"
            strokeLinecap="round"
            fill="none"
          />
        </>
      )
  }
}

/** Markings drawn above the body but below the eyes: muzzles, eye patches,
 * goggles, wings, hoods, bellies. */
function MascotMarkings({ id }: { id: MascotId }) {
  switch (id) {
    case 'fox':
      return <ellipse cx="48" cy="61" rx="13" ry="9" fill="white" opacity="0.45" />
    case 'bear':
      return <ellipse cx="48" cy="62" rx="12" ry="8" fill="white" opacity="0.45" />
    case 'cat':
      // Tabby stripes on the forehead.
      return (
        <g stroke={INK} strokeWidth="2.5" strokeLinecap="round" opacity="0.7">
          <path d="M48 27V37" />
          <path d="M40 29L42 38" />
          <path d="M56 29L54 38" />
        </g>
      )
    case 'bird':
      // Darker wings tucked against the sides.
      return (
        <>
          <ellipse cx="27" cy="62" rx="7" ry="13" fill={INK} opacity="0.22" transform="rotate(14 27 62)" />
          <ellipse cx="69" cy="62" rx="7" ry="13" fill={INK} opacity="0.22" transform="rotate(-14 69 62)" />
        </>
      )
    case 'dog':
      // One darker patch over the right eye.
      return <ellipse cx="58" cy="50" rx="9" ry="10" fill={INK} opacity="0.35" />
    case 'owl':
      // Big "goggle" eye rings touching in the middle, plus darker wings.
      return (
        <>
          <ellipse cx="22" cy="62" rx="7" ry="14" fill={INK} opacity="0.22" transform="rotate(12 22 62)" />
          <ellipse cx="74" cy="62" rx="7" ry="14" fill={INK} opacity="0.22" transform="rotate(-12 74 62)" />
          <circle cx="38" cy="50" r="10" fill="white" opacity="0.7" />
          <circle cx="58" cy="50" r="10" fill="white" opacity="0.7" />
          <circle cx="38" cy="50" r="10" stroke={INK} strokeWidth="2.5" />
          <circle cx="58" cy="50" r="10" stroke={INK} strokeWidth="2.5" />
        </>
      )
    case 'penguin':
      // Black hood over the head, black flippers, white belly — the
      // theme colour shows through on the face.
      return (
        <>
          <path d="M18 56C18 30 30 24 48 24C66 24 78 30 78 56C78 46 66 40 48 40C30 40 18 46 18 56Z" fill={INK} />
          <ellipse cx="21" cy="64" rx="6" ry="14" fill={INK} transform="rotate(14 21 64)" />
          <ellipse cx="75" cy="64" rx="6" ry="14" fill={INK} transform="rotate(-14 75 64)" />
          <ellipse cx="48" cy="69" rx="16" ry="12" fill="white" opacity="0.9" />
        </>
      )
    case 'panda':
      // Black eye patches (with a white eye inside so the pupil still
      // shows) and black arms hugging the lower body.
      return (
        <>
          <ellipse cx="24" cy="70" rx="9" ry="12" fill={INK} transform="rotate(25 24 70)" />
          <ellipse cx="72" cy="70" rx="9" ry="12" fill={INK} transform="rotate(-25 72 70)" />
          <ellipse cx="37" cy="51" rx="8.5" ry="10" fill={INK} transform="rotate(-20 37 51)" />
          <ellipse cx="59" cy="51" rx="8.5" ry="10" fill={INK} transform="rotate(20 59 51)" />
          <circle cx="38" cy="50" r="4.5" fill="white" />
          <circle cx="58" cy="50" r="4.5" fill="white" />
        </>
      )
    default:
      return null
  }
}

/** Noses, beaks, whiskers, tongues — drawn on top of everything. */
function MascotFront({ id }: { id: MascotId }) {
  switch (id) {
    case 'fox':
      return <path d="M44 57L52 57L48 61.5Z" fill={INK} />
    case 'bear':
      return <ellipse cx="48" cy="59" rx="5.5" ry="4" fill={INK} />
    case 'cat':
      return (
        <>
          <path d="M45 57L51 57L48 60.5Z" fill={PINK} />
          <g stroke={INK} strokeWidth="3" strokeLinecap="round">
            <path d="M30 58L10 53" />
            <path d="M30 62L10 62" />
            <path d="M30 66L10 71" />
            <path d="M66 58L86 53" />
            <path d="M66 62L86 62" />
            <path d="M66 66L86 71" />
          </g>
        </>
      )
    case 'bird':
      return <path d="M37 56L48 52L59 56L48 67Z" fill={BEAK} stroke={INK} strokeWidth="1.5" strokeLinejoin="round" />
    case 'rabbit':
      return (
        <>
          <ellipse cx="48" cy="60" rx="3" ry="2.2" fill={PINK} />
          <rect x="44" y="64.5" width="4" height="5.5" rx="1" fill="white" stroke={INK} strokeWidth="1" />
          <rect x="48" y="64.5" width="4" height="5.5" rx="1" fill="white" stroke={INK} strokeWidth="1" />
        </>
      )
    case 'dog':
      return (
        <>
          <ellipse cx="48" cy="58" rx="5" ry="3.5" fill={INK} />
          <path d="M45 65.5h6v4.5a3 3 0 0 1 -6 0Z" fill={TONGUE} stroke={INK} strokeWidth="1" />
        </>
      )
    case 'owl':
      return <path d="M44 59L52 59L48 67Z" fill={BEAK} stroke={INK} strokeWidth="1.5" strokeLinejoin="round" />
    case 'penguin':
      return <path d="M42 58L48 55L54 58L48 64Z" fill={BEAK} stroke={INK} strokeWidth="1.5" strokeLinejoin="round" />
    case 'panda':
      return <ellipse cx="48" cy="60" rx="4" ry="3" fill={INK} />
    case 'pig':
      return (
        <>
          <ellipse cx="48" cy="61" rx="12" ry="8" fill={PINK} stroke={INK} strokeWidth="1.2" />
          <circle cx="43.5" cy="61" r="1.8" fill={INK} />
          <circle cx="52.5" cy="61" r="1.8" fill={INK} />
        </>
      )
  }
}
