import { createContext, useContext, useReducer } from 'react'

const StoreContext = createContext()

const initialState = {
  messages: [],
  sessions: [],
  currentSession: '',
  swarm: null,
  processing: false,
  strategy: null,
  steps: [],
}

const initialSwarm = () => ({
  cycle: 0,
  agents: {},
  regions: [],
  activities: [],
  complete: false,
  elapsed: '',
  findings: 0,
  tokens: 0,
})

function getAgent(swarm, id) {
  if (!swarm.agents[id]) {
    swarm.agents[id] = { point: '', role: '', phase: 'nascent', done: false, dead: false, events: [], tokens: 0 }
  }
  return swarm.agents[id]
}

let activityCounter = 0
function addActivity(swarm, cls, icon, text) {
  swarm.activities = [...swarm.activities.slice(-49), { cls, icon, text, id: ++activityCounter }]
}

// Helper: extract meta fields from Go event. Event JSON is:
// {"type":"agent_spawn","content":"...","meta":{"agent":"a1","point":"engine"}}
function meta(ev, key) {
  return ev.meta?.[key] || ''
}

// Reconstruct persisted-style steps from live swarm state
function buildSwarmSteps(swarm) {
  const steps = []
  for (const [id, ag] of Object.entries(swarm.agents || {})) {
    steps.push({ kind: 'agent_spawn', agent: id, point: ag.point || '' })
    for (const ev of (ag.events || [])) {
      switch (ev.type) {
        case 'diff':
          steps.push({ kind: 'agent_diff', agent: id, role: ev.role || '' })
          break
        case 'move':
          steps.push({ kind: 'agent_move', agent: id, point: ev.to || '' })
          break
        case 'divide':
          steps.push({ kind: 'agent_divide', agent: id })
          break
        case 'tool':
          steps.push({ kind: 'tool_use', agent: id, tool: ev.tool || '', args: ev.args || '' })
          break
        case 'tool-result':
          steps.push({ kind: 'tool_result', agent: id, content: ev.content || '' })
          break
        case 'finding':
          steps.push({ kind: 'agent_done', agent: id, content: ev.content || '' })
          break
      }
    }
    if (ag.dead) steps.push({ kind: 'agent_death', agent: id })
  }
  if (swarm.complete) {
    steps.push({ kind: 'complete', content: swarm.elapsed || '' })
  }
  return steps
}

function reducer(state, action) {
  switch (action.type) {
    case 'ADD_MESSAGE':
      return { ...state, messages: [...state.messages, action.payload] }

    case 'SET_MESSAGES':
      return { ...state, messages: action.payload, swarm: null, processing: false, strategy: null, steps: [] }

    case 'SET_SESSIONS':
      return { ...state, sessions: action.payload.sessions || [], currentSession: action.payload.current || '' }

    case 'SET_PROCESSING':
      return { ...state, processing: action.payload }

    case 'SET_STRATEGY':
      return { ...state, strategy: action.payload }

    case 'CLEAR_STEPS':
      return { ...state, steps: [] }

    case 'SSE_EVENT': {
      const ev = action.payload

      switch (ev.type) {
        case 'assistant_message': {
          // Build the assistant message with any live steps/swarm data attached
          // so persisted intermediate steps render inline immediately
          const msg = { role: 'assistant', content: ev.content }
          if (state.swarm) {
            msg.strategy = 'swarm'
            msg.steps = buildSwarmSteps(state.swarm)
          } else if (state.steps.length > 0) {
            msg.strategy = state.strategy || 'assist'
            msg.steps = state.steps.map(s => ({
              kind: s.kind === 'tool' ? 'tool_use' : s.kind === 'result' ? 'tool_result' : s.kind,
              tool: s.tool || '',
              args: s.args || '',
              content: s.content || '',
            }))
          }
          return {
            ...state,
            messages: [...state.messages, msg],
            processing: false,
            swarm: null,
            steps: [],
            strategy: null,
          }
        }

        case 'thinking': {
          const match = ev.content?.match(/^\[(\w+)\]/)
          const strategy = match ? match[1] : state.strategy
          if (strategy === 'swarm' && !state.swarm) {
            return { ...state, strategy, swarm: initialSwarm(), steps: [] }
          }
          if (strategy !== 'swarm') {
            return { ...state, strategy, steps: [...state.steps, { kind: 'thinking', content: ev.content }] }
          }
          return { ...state, strategy }
        }

        case 'tick_start': {
          const swarm = state.swarm ? { ...state.swarm } : initialSwarm()
          swarm.cycle = (swarm.cycle || 0) + 1
          return { ...state, swarm, strategy: 'swarm' }
        }

        case 'field_state': {
          if (!state.swarm) return state
          try {
            const regions = JSON.parse(ev.content)
            const swarm = { ...state.swarm, regions }
            for (const r of regions) {
              for (const a of (r.agents || [])) {
                const ag = getAgent(swarm, a.id)
                ag.point = r.id
                if (a.role) ag.role = a.role
                if (a.phase) ag.phase = a.phase
              }
            }
            return { ...state, swarm }
          } catch { return state }
        }

        case 'agent_spawn': {
          if (!state.swarm) return state
          const agent = meta(ev, 'agent')
          const point = meta(ev, 'point')
          const swarm = { ...state.swarm, agents: { ...state.swarm.agents } }
          const ag = getAgent(swarm, agent)
          ag.point = point
          ag.phase = 'nascent'
          addActivity(swarm, 'spawn', '+', `${agent} spawned at ${point || '?'}`)
          return { ...state, swarm }
        }

        case 'agent_differentiate': {
          if (!state.swarm) return state
          const agent = meta(ev, 'agent')
          const role = meta(ev, 'role')
          const swarm = { ...state.swarm, agents: { ...state.swarm.agents } }
          const ag = getAgent(swarm, agent)
          ag.role = role
          ag.phase = 'working'
          ag.events = [...ag.events, { type: 'diff', role }]
          addActivity(swarm, 'diff', '~', `${agent} -> ${role || '?'}`)
          return { ...state, swarm }
        }

        case 'agent_move': {
          if (!state.swarm) return state
          const agent = meta(ev, 'agent')
          const point = meta(ev, 'point')
          const swarm = { ...state.swarm, agents: { ...state.swarm.agents } }
          const ag = getAgent(swarm, agent)
          const from = ag.point || '?'
          ag.point = point
          ag.events = [...ag.events, { type: 'move', from, to: point }]
          addActivity(swarm, 'move', '\u21e0', `${agent} ${from} -> ${point || '?'}`)
          return { ...state, swarm }
        }

        case 'agent_divide': {
          if (!state.swarm) return state
          const agent = meta(ev, 'agent')
          const swarm = { ...state.swarm, agents: { ...state.swarm.agents } }
          const ag = getAgent(swarm, agent)
          ag.events = [...ag.events, { type: 'divide' }]
          addActivity(swarm, 'divide', '\u2726', `${agent} divided`)
          return { ...state, swarm }
        }

        case 'agent_work_done': {
          if (!state.swarm) return state
          const agent = meta(ev, 'agent')
          const tok = parseInt(meta(ev, 'tokens')) || 0
          const swarm = { ...state.swarm, agents: { ...state.swarm.agents }, tokens: (state.swarm.tokens || 0) + tok }
          const ag = getAgent(swarm, agent)
          ag.done = true
          ag.phase = 'resting'
          ag.tokens = (ag.tokens || 0) + tok
          if (ev.content) ag.events = [...ag.events, { type: 'finding', content: ev.content }]
          addActivity(swarm, 'done', '\u2713', `${agent} finished`)
          return { ...state, swarm }
        }

        case 'agent_death': {
          if (!state.swarm) return state
          const agent = meta(ev, 'agent')
          const swarm = { ...state.swarm, agents: { ...state.swarm.agents } }
          const ag = getAgent(swarm, agent)
          ag.dead = true
          ag.phase = 'apoptotic'
          addActivity(swarm, 'death', '\u2717', `${agent} died`)
          return { ...state, swarm }
        }

        case 'tool_use': {
          const agent = meta(ev, 'agent')
          const tool = meta(ev, 'tool')
          const args = meta(ev, 'args')
          if (!state.swarm) {
            const argsShort = args.length > 80 ? args.substring(0, 80) + '...' : args
            return { ...state, steps: [...state.steps, { kind: 'tool', tool, args: argsShort }] }
          }
          if (agent) {
            const swarm = { ...state.swarm, agents: { ...state.swarm.agents } }
            const ag = getAgent(swarm, agent)
            const argsShort = args.length > 60 ? args.substring(0, 60) + '...' : args
            ag.events = [...ag.events, { type: 'tool', tool, args: argsShort }]
            return { ...state, swarm }
          }
          return state
        }

        case 'tool_result': {
          const agent = meta(ev, 'agent')
          if (!state.swarm && !agent) {
            const short = ev.content?.length > 120 ? ev.content.substring(0, 120) + '...' : (ev.content || '')
            return { ...state, steps: [...state.steps, { kind: 'result', content: short }] }
          }
          if (state.swarm && agent) {
            const swarm = { ...state.swarm, agents: { ...state.swarm.agents } }
            const ag = getAgent(swarm, agent)
            const short = ev.content?.length > 120 ? ev.content.substring(0, 120) + '...' : (ev.content || '')
            ag.events = [...ag.events, { type: 'tool-result', content: short }]
            return { ...state, swarm }
          }
          return state
        }

        case 'run_complete': {
          if (!state.swarm) return state
          return {
            ...state,
            swarm: {
              ...state.swarm,
              complete: true,
              elapsed: meta(ev, 'elapsed'),
              findings: parseInt(meta(ev, 'findings')) || 0,
              tokens: parseInt(meta(ev, 'tokens')) || 0,
            },
            processing: false,
          }
        }

        default:
          return state
      }
    }

    default:
      return state
  }
}

export function StoreProvider({ children }) {
  const [state, dispatch] = useReducer(reducer, initialState)
  return (
    <StoreContext.Provider value={{ state, dispatch }}>
      {children}
    </StoreContext.Provider>
  )
}

export function useStore() {
  return useContext(StoreContext)
}
