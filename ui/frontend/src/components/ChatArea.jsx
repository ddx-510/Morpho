import React, { useState, useRef, useEffect } from 'react'
import { marked } from 'marked'
import { Send, Leaf, Sparkles } from 'lucide-react'
import { useStore } from '../store'
import MorphoMessage from './MorphoMessage'
import SwarmCard from './SwarmCard'
import StepGroup from './StepGroup'
import './ChatArea.css'

function md(text) {
  if (!text) return ''
  return marked.parse(text)
}

export default function ChatArea() {
  const { state, dispatch } = useStore()
  const [input, setInput] = useState('')
  const msgsRef = useRef(null)
  const textareaRef = useRef(null)

  useEffect(() => {
    if (msgsRef.current) {
      msgsRef.current.scrollTop = msgsRef.current.scrollHeight
    }
  }, [state.messages, state.swarm, state.steps])

  const autoGrow = () => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    const h = Math.min(el.scrollHeight, 200)
    el.style.height = h + 'px'
    el.classList.toggle('scrollable', el.scrollHeight > 200)
  }

  const send = async () => {
    const msg = input.trim()
    if (!msg || state.processing) return
    setInput('')
    if (textareaRef.current) textareaRef.current.style.height = 'auto'
    dispatch({ type: 'ADD_MESSAGE', payload: { role: 'user', content: msg } })
    dispatch({ type: 'SET_PROCESSING', payload: true })
    dispatch({ type: 'CLEAR_STEPS' })

    try {
      await fetch('/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: msg }),
      })
    } catch {
      dispatch({ type: 'SET_PROCESSING', payload: false })
    }
  }

  const handleKey = (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

  const strategyBadge = state.strategy ? (
    <span className={`badge ${state.strategy}`}>
      <Sparkles size={10} />
      {state.strategy}
    </span>
  ) : null

  // Find where to insert live content
  const liveInsertIdx = (state.swarm || state.steps.length > 0 || state.processing)
    ? findLastUserIdx(state.messages) : -1

  const hasMessages = state.messages.length > 0 || state.swarm || state.steps.length > 0

  return (
    <div className="main">
      <div className="chat-header">
        <span className="title">Morpho</span>
        {strategyBadge}
      </div>

      {!hasMessages ? (
        <div className="empty-state">
          <div className="empty-icon"><Leaf size={28} /></div>
          <div className="empty-title">Start a conversation</div>
          <div className="empty-sub">
            Ask Morpho to analyze code, review projects, or answer questions.
            Complex tasks automatically scale into a multi-agent swarm.
          </div>
        </div>
      ) : (
        <div className="messages" ref={msgsRef}>
          <div className="messages-inner">
            {state.messages.map((msg, i) => (
              <React.Fragment key={i}>
                {/* Persisted intermediate steps before assistant response */}
                {msg.role === 'assistant' && msg.steps && msg.steps.length > 0 && (
                  <MorphoMessage label={msg.strategy === 'swarm' ? 'swarm' : 'working'}>
                    {msg.strategy === 'swarm'
                      ? <SwarmCard steps={msg.steps} />
                      : <StepGroup steps={msg.steps.map(s => ({
                          kind: s.kind === 'tool_use' ? 'tool' : s.kind === 'tool_result' ? 'result' : s.kind,
                          tool: s.tool,
                          args: s.args,
                          content: s.content,
                        }))} />
                    }
                  </MorphoMessage>
                )}

                {/* The message itself */}
                {msg.role === 'user' ? (
                  <div className="msg-user">
                    <div className="user-bubble">{msg.content}</div>
                  </div>
                ) : (
                  <MorphoMessage>
                    <div
                      className="md-content"
                      dangerouslySetInnerHTML={{ __html: md(msg.content) }}
                    />
                  </MorphoMessage>
                )}

                {/* Live content after the triggering user message */}
                {i === liveInsertIdx && (
                  <>
                    {state.swarm && (
                      <MorphoMessage label="swarm">
                        <SwarmCard />
                      </MorphoMessage>
                    )}
                    {!state.swarm && state.steps.length > 0 && (
                      <MorphoMessage label="working">
                        <StepGroup steps={state.steps} />
                      </MorphoMessage>
                    )}
                    {state.processing && !state.swarm && state.steps.length === 0 && (
                      <MorphoMessage>
                        <div className="processing-indicator">
                          <div className="processing-dots">
                            <span /><span /><span />
                          </div>
                          <span className="processing-text">thinking...</span>
                        </div>
                      </MorphoMessage>
                    )}
                  </>
                )}
              </React.Fragment>
            ))}

            {/* Fallback: live content when no messages yet */}
            {liveInsertIdx === -1 && (
              <>
                {state.swarm && (
                  <MorphoMessage label="swarm">
                    <SwarmCard />
                  </MorphoMessage>
                )}
                {!state.swarm && state.steps.length > 0 && (
                  <MorphoMessage label="working">
                    <StepGroup steps={state.steps} />
                  </MorphoMessage>
                )}
                {state.processing && !state.swarm && state.steps.length === 0 && (
                  <MorphoMessage>
                    <div className="processing-indicator">
                      <div className="processing-dots">
                        <span /><span /><span />
                      </div>
                      <span className="processing-text">thinking...</span>
                    </div>
                  </MorphoMessage>
                )}
              </>
            )}
          </div>
        </div>
      )}

      <div className="input-area">
        <div className="input-container">
          <textarea
            ref={textareaRef}
            value={input}
            onChange={e => { setInput(e.target.value); autoGrow() }}
            onKeyDown={handleKey}
            placeholder="Message Morpho..."
            rows={1}
            disabled={state.processing}
          />
          <button className="send-btn" onClick={send} disabled={state.processing || !input.trim()}>
            <Send size={16} />
          </button>
        </div>
      </div>
    </div>
  )
}

function findLastUserIdx(messages) {
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].role === 'user') return i
  }
  return -1
}
