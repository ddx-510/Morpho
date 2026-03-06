import { useState } from 'react'
import { motion } from 'framer-motion'
import { marked } from 'marked'
import { X } from 'lucide-react'
import './AgentDetail.css'

function formatTokens(n) {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return String(n)
}

function FindingBlock({ content }) {
  const [open, setOpen] = useState(false)
  const plain = (content || '').replace(/[#*`_~>\-]/g, '').replace(/\s+/g, ' ').trim()
  const preview = plain.length > 100 ? plain.substring(0, 100) + '...' : plain

  return (
    <div className="finding-block">
      <div className="finding-toggle" onClick={() => setOpen(!open)}>
        <span className={`finding-chevron ${open ? 'open' : ''}`}>{'\u25b8'}</span>
        {!open && <span className="finding-summary">{preview}</span>}
        {open && <span className="finding-summary">Finding</span>}
      </div>
      {open && (
        <div className="finding-body md-content" dangerouslySetInnerHTML={{ __html: marked.parse(content || '') }} />
      )}
    </div>
  )
}

export default function AgentDetail({ id, agent, onClose }) {
  return (
    <motion.div
      className="agent-detail"
      initial={{ opacity: 0, height: 0 }}
      animate={{ opacity: 1, height: 'auto' }}
      exit={{ opacity: 0, height: 0 }}
      transition={{ duration: 0.25 }}
    >
      <div className="agent-detail-header">
        <span className="agent-detail-title">
          {id} — {agent.role || 'undiff'} @ {agent.point || '?'}
        </span>
        {agent.tokens > 0 && (
          <span className="agent-detail-tokens">{formatTokens(agent.tokens)} tok</span>
        )}
        <button className="agent-detail-close" onClick={onClose}><X size={14} /></button>
      </div>
      <div className="agent-detail-events">
        {(!agent.events || agent.events.length === 0) ? (
          <div className="no-events">No activity yet</div>
        ) : (
          agent.events.map((ev, i) => (
            <div key={i} className={`agent-event ${ev.type}`}>
              {ev.type === 'tool' && (
                <>{'\u25b8'} {ev.tool} <span className="ev-args">{ev.args}</span></>
              )}
              {ev.type === 'tool-result' && (
                <span className="ev-result">{ev.content}</span>
              )}
              {ev.type === 'finding' && (
                <FindingBlock content={ev.content} />
              )}
              {ev.type === 'diff' && (
                <>{'\u2192'} differentiated: {ev.role}</>
              )}
              {ev.type === 'move' && (
                <>{'\u21e2'} moved {ev.from} {'\u2192'} {ev.to}</>
              )}
              {ev.type === 'divide' && (
                <>{'\u2726'} mitosis</>
              )}
            </div>
          ))
        )}
      </div>
    </motion.div>
  )
}
