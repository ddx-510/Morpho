import { useMemo, useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Zap, Grid3X3, Map, HelpCircle } from 'lucide-react'
import { useStore } from '../store'
import OrganismTank from './OrganismTank'
import FieldView from './FieldView'
import AgentDetail from './AgentDetail'
import ActivityFeed from './ActivityFeed'
import './SwarmCard.css'

function buildFromSteps(steps) {
  const agents = {}
  let elapsed = ''
  let findingsCount = 0

  for (const s of steps) {
    switch (s.kind) {
      case 'agent_spawn':
        if (s.agent) agents[s.agent] = {
          point: s.point || '', role: '', phase: 'nascent',
          done: false, dead: false, events: []
        }
        break
      case 'agent_diff':
        if (s.agent && agents[s.agent]) {
          agents[s.agent].role = s.role || ''
          agents[s.agent].phase = 'working'
          agents[s.agent].events.push({ type: 'diff', role: s.role })
        }
        break
      case 'agent_move':
        if (s.agent && agents[s.agent]) {
          agents[s.agent].point = s.point || agents[s.agent].point
          agents[s.agent].events.push({ type: 'move', from: '?', to: s.point })
        }
        break
      case 'tool_use':
        if (s.agent && agents[s.agent])
          agents[s.agent].events.push({ type: 'tool', tool: s.tool || '', args: s.args || '' })
        break
      case 'tool_result':
        if (s.agent && agents[s.agent])
          agents[s.agent].events.push({ type: 'tool-result', content: s.content || '' })
        break
      case 'agent_done':
        if (s.agent && agents[s.agent]) {
          agents[s.agent].done = true
          agents[s.agent].phase = 'resting'
          if (s.content) {
            agents[s.agent].events.push({ type: 'finding', content: s.content })
            findingsCount++
          }
        }
        break
      case 'agent_death':
        if (s.agent && agents[s.agent]) {
          agents[s.agent].dead = true
          agents[s.agent].phase = 'apoptotic'
        }
        break
      case 'agent_divide':
        if (s.agent && agents[s.agent])
          agents[s.agent].events.push({ type: 'divide' })
        break
      case 'complete':
        elapsed = s.content || ''
        break
    }
  }

  const regionMap = {}
  for (const [, ag] of Object.entries(agents)) {
    const pt = ag.point || 'unknown'
    if (!regionMap[pt]) regionMap[pt] = { id: pt, signals: {} }
  }

  return {
    agents,
    regions: Object.values(regionMap),
    complete: true,
    elapsed,
    findings: findingsCount,
    cycle: 0,
    tokens: 0,
    activities: [],
  }
}

export default function SwarmCard({ steps }) {
  const { state } = useStore()
  const [selectedAgent, setSelectedAgent] = useState(null)
  const [view, setView] = useState('agents')
  const [showLegend, setShowLegend] = useState(false)

  const persisted = useMemo(() => steps ? buildFromSteps(steps) : null, [steps])

  const swarm = persisted || state.swarm
  if (!swarm) return null

  const aliveCount = Object.values(swarm.agents).filter(a => !a.dead).length
  const workingCount = Object.values(swarm.agents).filter(a => a.phase === 'working').length
  const findingsCount = Object.values(swarm.agents).filter(a => a.done).length
  const agentCount = Object.keys(swarm.agents).length
  const onAgentClick = (id) => setSelectedAgent(selectedAgent === id ? null : id)

  const Wrapper = persisted ? 'div' : motion.div
  const wrapperProps = persisted
    ? { className: 'swarm-card' }
    : { className: 'swarm-card', initial: { opacity: 0, y: 12 }, animate: { opacity: 1, y: 0 }, transition: { duration: 0.3 } }

  return (
    <Wrapper {...wrapperProps}>
      <div className="swarm-card-header">
        <span className="swarm-icon"><Zap size={14} /></span>
        <span>Swarm</span>

        <div className="swarm-view-toggle">
          <button className={`view-tab ${view === 'agents' ? 'active' : ''}`} onClick={() => setView('agents')}>
            <Grid3X3 size={11} />
            <span>Agents</span>
          </button>
          <button className={`view-tab ${view === 'field' ? 'active' : ''}`} onClick={() => setView('field')}>
            <Map size={11} />
            <span>Field</span>
          </button>
        </div>

        <span className={`swarm-status ${swarm.complete ? 'complete' : 'running'}`}>
          {swarm.complete
            ? `${swarm.findings} findings${swarm.elapsed ? ` \u00b7 ${swarm.elapsed}` : ''}${swarm.tokens > 0 ? ` \u00b7 ${formatTokens(swarm.tokens)} tok` : ''}`
            : 'running'}
        </span>
      </div>

      <div className="swarm-stats-bar">
        <div className="stat-item findings">
          <span className="stat-val">{findingsCount}</span>
          <span className="stat-label">Findings</span>
        </div>
        <div className="stat-item alive">
          <span className="stat-val">{persisted ? `${aliveCount}/${agentCount}` : aliveCount}</span>
          <span className="stat-label">{persisted ? 'Agents' : 'Alive'}</span>
        </div>
        {workingCount > 0 && (
          <div className="stat-item working">
            <span className="stat-val">{workingCount}</span>
            <span className="stat-label">Working</span>
          </div>
        )}
        {!persisted && (
          <div className="stat-item tick">
            <span className="stat-val">{swarm.cycle || 0}</span>
            <span className="stat-label">Tick</span>
          </div>
        )}
        {swarm.elapsed && !persisted && (
          <div className="stat-item elapsed">
            <span className="stat-val">{swarm.elapsed}</span>
            <span className="stat-label">Elapsed</span>
          </div>
        )}
        {(swarm.tokens || 0) > 0 && (
          <div className="stat-item tokens">
            <span className="stat-val">{formatTokens(swarm.tokens)}</span>
            <span className="stat-label">Tokens</span>
          </div>
        )}
        <div className="stats-end">
          {!swarm.complete && aliveCount > 0 && (
            <div className="bio-pulse" />
          )}
          <button
            className={`swarm-legend-toggle ${showLegend ? 'open' : ''}`}
            onClick={() => setShowLegend(!showLegend)}
            title="Cell legend"
          >
            <HelpCircle size={12} />
          </button>
        </div>
      </div>

      <div className={`swarm-legend ${showLegend ? 'open' : ''}`}>
        <div className="legend-item"><div className="legend-cell nascent" /><span>Stem cell</span></div>
        <div className="legend-item"><div className="legend-cell working" /><span>Working</span></div>
        <div className="legend-item"><div className="legend-cell resting" /><span>Resting</span></div>
        <div className="legend-item"><div className="legend-cell dead" /><span>Dead</span></div>
        <div className="legend-item"><div className="legend-badge">2</div><span>Findings</span></div>
      </div>

      <AnimatePresence mode="wait">
        {view === 'agents' ? (
          <motion.div key="agents" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: 0.15 }}>
            <OrganismTank agents={swarm.agents} complete={swarm.complete} selectedAgent={selectedAgent} onAgentClick={onAgentClick} />
          </motion.div>
        ) : (
          <motion.div key="field" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: 0.15 }}>
            <FieldView regions={swarm.regions} agents={swarm.agents} selectedAgent={selectedAgent} onAgentClick={onAgentClick} />
          </motion.div>
        )}
      </AnimatePresence>

      {!persisted && swarm.activities && swarm.activities.length > 0 && (
        <ActivityFeed activities={swarm.activities} />
      )}

      <AnimatePresence>
        {selectedAgent && swarm.agents[selectedAgent] && (
          <AgentDetail id={selectedAgent} agent={swarm.agents[selectedAgent]} onClose={() => setSelectedAgent(null)} />
        )}
      </AnimatePresence>
    </Wrapper>
  )
}

function formatTokens(n) {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return String(n)
}
