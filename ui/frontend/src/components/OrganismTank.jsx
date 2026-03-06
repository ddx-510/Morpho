import { motion, AnimatePresence } from 'framer-motion'
import { Bot } from 'lucide-react'
import { roleColor } from '../utils/colors'
import './OrganismTank.css'

// Cell size based on phase.
function cellSize(agent) {
  if (agent.dead) return 18
  switch (agent.phase) {
    case 'nascent':  return 22
    case 'seeking':  return 24
    case 'working':  return 30
    case 'resting':  return 26
    default: return 24
  }
}

function Organism({ id, agent, selected, onClick }) {
  const color = roleColor(agent.role)
  const size = cellSize(agent)
  const isDead = agent.dead
  const isWorking = agent.phase === 'working'
  const isNascent = agent.phase === 'nascent'
  const evs = agent.events || []
  const findingCount = evs.filter(e => e.type === 'finding').length

  return (
    <motion.div
      className={`cell ${agent.phase || ''} ${isDead ? 'dead' : ''} ${selected ? 'selected' : ''}`}
      onClick={onClick}
      title={`${id} — ${agent.role || 'stem cell'} @ ${agent.point || '?'} [${agent.phase}]`}
      initial={{ opacity: 0, scale: 0 }}
      animate={{ opacity: isDead ? 0.35 : 1, scale: 1 }}
      exit={{ opacity: 0, scale: 0, transition: { duration: 0.3 } }}
      transition={{ scale: { type: 'spring', stiffness: 300, damping: 20 } }}
    >
      <motion.div
        className="cell-body"
        style={{
          width: size,
          height: size,
          background: isDead
            ? 'var(--border)'
            : agent.role
              ? `radial-gradient(circle at 35% 35%, ${color}30, ${color}bb)`
              : 'radial-gradient(circle at 35% 35%, var(--mint-200), var(--mint-400))',
          borderColor: isDead ? 'transparent' : 'var(--mint-300)',
          boxShadow: isWorking ? `0 0 8px ${color}40` : 'none',
        }}
        animate={
          isWorking ? { scale: [1, 1.1, 1] }
          : isNascent ? { opacity: [0.5, 1, 0.5] }
          : {}
        }
        transition={
          isWorking ? { duration: 1.5, repeat: Infinity, ease: 'easeInOut' }
          : isNascent ? { duration: 2, repeat: Infinity, ease: 'easeInOut' }
          : {}
        }
      >
        {agent.role && !isDead && (
          <div className="cell-nucleus" style={{ background: color }} />
        )}
        {findingCount > 0 && (
          <div className="cell-badge">{findingCount}</div>
        )}
      </motion.div>
      <span className="cell-id">{id}</span>
    </motion.div>
  )
}

export default function OrganismTank({ agents, complete, selectedAgent, onAgentClick }) {
  const entries = Object.entries(agents || {})

  if (entries.length === 0) {
    return (
      <div className="tank">
        <div className="tank-empty">
          <Bot size={16} />
          <span>Spawning agents...</span>
        </div>
      </div>
    )
  }

  // Group by region.
  const byRegion = {}
  for (const [id, ag] of entries) {
    const region = ag.point || 'unknown'
    if (!byRegion[region]) byRegion[region] = []
    byRegion[region].push([id, ag])
  }
  const regionEntries = Object.entries(byRegion)

  return (
    <div className={`tank ${complete ? 'done' : ''}`}>
      <div className="tank-grid">
        {regionEntries.map(([region, regionAgents]) => (
          <div key={region} className="tank-region">
            <div className="tank-region-head">
              <span className="tank-region-name">{region === '.' ? 'root' : region}</span>
              <span className="tank-region-count">{regionAgents.length}</span>
            </div>
            <div className="tank-region-cells">
              <AnimatePresence>
                {regionAgents.map(([id, ag]) => (
                  <Organism
                    key={id}
                    id={id}
                    agent={ag}
                    selected={selectedAgent === id}
                    onClick={() => onAgentClick(id)}
                  />
                ))}
              </AnimatePresence>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
