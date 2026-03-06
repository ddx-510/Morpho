import { useState } from 'react'
import { Wrench } from 'lucide-react'
import './StepGroup.css'

export default function StepGroup({ steps }) {
  const [open, setOpen] = useState(false)

  return (
    <div className={`step-group ${open ? 'open' : ''}`}>
      <div className="step-header" onClick={() => setOpen(!open)}>
        <span className="step-arrow">{'\u25b6'}</span>
        <Wrench size={11} />
        <span className="step-label">Working</span>
        <span className="step-count">{steps.length}</span>
      </div>
      {open && (
        <div className="step-body">
          {steps.map((s, i) => (
            <div key={i} className={`step-line ${s.kind}`}>
              {s.kind === 'tool' && <>{'\u25b8'} {s.tool} {s.args}</>}
              {s.kind === 'result' && s.content}
              {s.kind === 'thinking' && <em>{s.content}</em>}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
