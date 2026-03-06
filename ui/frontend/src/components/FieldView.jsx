import { motion } from 'framer-motion'
import { roleColor, signalColor } from '../utils/colors'
import './SwarmCard.css'

export default function FieldView({ regions, agents, selectedAgent, onAgentClick }) {
  if (!regions || regions.length === 0) {
    return <div className="field-empty">No field data yet...</div>
  }

  return (
    <div className="field-grid">
      {regions.map(r => {
        const regionAgents = Object.entries(agents || {}).filter(([, a]) => a.point === r.id)
        const hasWorking = regionAgents.some(([, a]) => a.phase === 'working')
        const signals = Object.entries(r.signals || {}).sort((a, b) => b[1] - a[1])

        return (
          <motion.div
            key={r.id}
            className={`field-region ${hasWorking ? 'field-working' : ''} ${regionAgents.length > 0 ? 'field-occupied' : ''}`}
            initial={{ opacity: 0, scale: 0.95 }}
            animate={{ opacity: 1, scale: 1 }}
            transition={{ duration: 0.2 }}
          >
            <div className="field-region-head">
              <span className="field-region-name">{r.id === '.' ? 'root' : r.id}</span>
              {regionAgents.length > 0 && (
                <span className="field-agent-count">{regionAgents.length}</span>
              )}
            </div>

            {signals.length > 0 && (
              <div className="field-signals">
                {signals.slice(0, 6).map(([name, val]) => (
                  <div key={name} className="field-signal-row">
                    <span className="field-signal-label">{name}</span>
                    <div className="field-signal-track">
                      <motion.div
                        className="field-signal-fill"
                        style={{ background: signalColor(name) }}
                        initial={{ width: 0 }}
                        animate={{ width: `${Math.min(val * 100, 100)}%` }}
                        transition={{ duration: 0.4 }}
                      />
                    </div>
                    <span className="field-signal-val">{val.toFixed(2)}</span>
                  </div>
                ))}
              </div>
            )}

            {regionAgents.length > 0 && (
              <div className="field-agents">
                {regionAgents.map(([id, ag]) => (
                  <div
                    key={id}
                    className={`field-agent-dot ${ag.phase} ${selectedAgent === id ? 'selected' : ''} ${ag.dead ? 'dead' : ''}`}
                    style={{ background: ag.dead ? 'var(--border)' : roleColor(ag.role) }}
                    title={`${id} - ${ag.role || 'stem'} [${ag.phase}]`}
                    onClick={() => onAgentClick(id)}
                  />
                ))}
              </div>
            )}
          </motion.div>
        )
      })}
    </div>
  )
}
