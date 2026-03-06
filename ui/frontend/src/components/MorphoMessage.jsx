import { Leaf } from 'lucide-react'

export default function MorphoMessage({ children, label }) {
  return (
    <div className="msg-assistant">
      <div className="assistant-header">
        <div className="assistant-avatar"><Leaf size={13} /></div>
        <span className="assistant-name">Morpho</span>
        {label && <span className="assistant-label">{label}</span>}
      </div>
      <div className="assistant-body">
        {children}
      </div>
    </div>
  )
}
