import { useState, useRef, useEffect } from 'react'
import { MessageSquare, Plus, Pencil, Trash2, Leaf, AlertTriangle, PanelLeftClose, PanelLeftOpen } from 'lucide-react'
import { useStore } from '../store'
import './Sidebar.css'

export default function Sidebar({ width, collapsed, onToggle, onDragStart }) {
  const { state, dispatch } = useStore()
  const [deleteTarget, setDeleteTarget] = useState(null)
  const [renaming, setRenaming] = useState(null)
  const [renameValue, setRenameValue] = useState('')
  const renameRef = useRef(null)

  useEffect(() => {
    if (renaming && renameRef.current) {
      renameRef.current.focus()
      renameRef.current.select()
    }
  }, [renaming])

  const refreshSessions = async () => {
    const sr = await fetch('/api/sessions')
    const sd = await sr.json()
    dispatch({ type: 'SET_SESSIONS', payload: sd })
  }

  const newSession = async () => {
    await fetch('/api/sessions/new', { method: 'POST' })
    dispatch({ type: 'SET_MESSAGES', payload: [] })
    await refreshSessions()
  }

  const loadSession = async (id) => {
    if (renaming) return
    if (id === state.currentSession && (state.processing || state.swarm)) return
    const res = await fetch('/api/sessions/load', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id }),
    })
    const data = await res.json()
    dispatch({ type: 'SET_MESSAGES', payload: data.messages || [] })
    await refreshSessions()
  }

  const confirmDelete = async () => {
    if (!deleteTarget) return
    await fetch('/api/sessions/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: deleteTarget }),
    })
    setDeleteTarget(null)
    await refreshSessions()
  }

  const startRename = (e, id, currentTitle) => {
    e.stopPropagation()
    setRenaming(id)
    setRenameValue(currentTitle || '')
  }

  const submitRename = async (id) => {
    const name = renameValue.trim()
    if (name) {
      await fetch('/api/sessions/rename', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id, name }),
      })
      await refreshSessions()
    }
    setRenaming(null)
  }

  const handleRenameKey = (e, id) => {
    if (e.key === 'Enter') { e.preventDefault(); submitRename(id) }
    else if (e.key === 'Escape') setRenaming(null)
  }

  return (
    <div className={`sidebar ${collapsed ? 'collapsed' : ''}`} style={collapsed ? undefined : { width }}>
      <div className="sidebar-header">
        {!collapsed && (
          <div className="sidebar-logo">
            <div className="sidebar-logo-icon"><Leaf size={16} /></div>
            <h1>MORPHO</h1>
          </div>
        )}
        <button className="sidebar-toggle" onClick={onToggle} title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}>
          {collapsed ? <PanelLeftOpen size={14} /> : <PanelLeftClose size={14} />}
        </button>
      </div>

      <button className="new-chat-btn" onClick={newSession} title="New chat">
        <Plus size={14} />
        {!collapsed && <span>New chat</span>}
      </button>

      {!collapsed && (
        <div className="sessions-list">
          {state.sessions.map(s => (
            <div
              key={s.id}
              className={`session-item ${s.id === state.currentSession ? 'active' : ''}`}
              onClick={() => loadSession(s.id)}
            >
              <div className="session-icon">
                <MessageSquare size={14} />
              </div>
              <div className="session-info">
                {renaming === s.id ? (
                  <input
                    ref={renameRef}
                    className="rename-input"
                    value={renameValue}
                    onChange={e => setRenameValue(e.target.value)}
                    onKeyDown={e => handleRenameKey(e, s.id)}
                    onBlur={() => submitRename(s.id)}
                    onClick={e => e.stopPropagation()}
                  />
                ) : (
                  <div className="s-title" onDoubleClick={e => startRename(e, s.id, s.title)}>
                    {s.title || s.id.substring(0, 8)}
                  </div>
                )}
                <div className="s-meta">{s.message_count || 0} msgs</div>
              </div>
              <div className="session-actions">
                <button
                  className="session-rename"
                  onClick={e => startRename(e, s.id, s.title)}
                  title="Rename"
                >
                  <Pencil size={12} />
                </button>
                <button
                  className="session-delete"
                  onClick={e => { e.stopPropagation(); setDeleteTarget(s.id) }}
                  title="Delete"
                >
                  <Trash2 size={12} />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {collapsed && (
        <div className="sessions-list collapsed-sessions">
          {state.sessions.map(s => (
            <div
              key={s.id}
              className={`session-item ${s.id === state.currentSession ? 'active' : ''}`}
              onClick={() => loadSession(s.id)}
              title={s.title || s.id.substring(0, 8)}
            >
              <div className="session-icon">
                <MessageSquare size={14} />
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Drag handle for resize */}
      {!collapsed && <div className="sidebar-drag" onMouseDown={onDragStart} />}

      {/* Delete confirmation modal */}
      {deleteTarget && (
        <div className="modal-overlay" onClick={() => setDeleteTarget(null)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
              <div style={{
                width: 36, height: 36, borderRadius: 'var(--radius-md)',
                background: 'rgba(220,38,38,0.08)', display: 'flex',
                alignItems: 'center', justifyContent: 'center', color: 'var(--red)'
              }}>
                <AlertTriangle size={18} />
              </div>
              <div className="modal-title">Delete conversation?</div>
            </div>
            <div className="modal-body">
              This will permanently delete this conversation and all its messages. This action cannot be undone.
            </div>
            <div className="modal-actions">
              <button className="modal-btn" onClick={() => setDeleteTarget(null)}>Cancel</button>
              <button className="modal-btn danger" onClick={confirmDelete}>Delete</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
