// Hash-based color generation for dynamic roles and signals.

export function roleColor(role) {
  if (!role) return '#8aaa95'
  let hash = 0
  for (let i = 0; i < role.length; i++) hash = role.charCodeAt(i) + ((hash << 5) - hash)
  const hue = Math.abs(hash) % 360
  return `hsl(${hue}, 55%, 45%)`
}

export function signalColor(sig) {
  if (!sig) return 'var(--mint-400)'
  let hash = 0
  for (let i = 0; i < sig.length; i++) hash = sig.charCodeAt(i) + ((hash << 5) - hash)
  const hue = Math.abs(hash) % 360
  return `hsl(${hue}, 50%, 50%)`
}
