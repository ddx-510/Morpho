import { useRef, useEffect } from 'react'
import './ActivityFeed.css'

const icons = {
  spawn:  '\u002B',  // +
  diff:   '\u2192',  // →
  move:   '\u21C4',  // ⇄
  done:   '\u2713',  // ✓
  death:  '\u00D7',  // ×
  divide: '\u2726',  // ✦
}

export default function ActivityFeed({ activities }) {
  const ref = useRef(null)

  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight
  }, [activities.length])

  if (activities.length === 0) return null

  return (
    <div className="activity-feed" ref={ref}>
      {activities.map(a => (
        <div key={a.id} className={`activity-pill ${a.cls}`}>
          <span className="act-icon">{icons[a.cls] || a.icon}</span>
          <span className="act-text">{a.text}</span>
        </div>
      ))}
    </div>
  )
}
