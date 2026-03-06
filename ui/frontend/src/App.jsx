import { useCallback, useEffect, useState, useRef } from 'react'
import { StoreProvider, useStore } from './store'
import { useSSE } from './hooks/useSSE'
import Sidebar from './components/Sidebar'
import ChatArea from './components/ChatArea'

function AppInner() {
  const { state, dispatch } = useStore()
  const [sidebarWidth, setSidebarWidth] = useState(() => {
    const saved = localStorage.getItem('morpho-sidebar-width')
    return saved ? Number(saved) : 272
  })
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem('morpho-sidebar-collapsed') === 'true')
  const dragging = useRef(false)

  const toggleCollapsed = () => {
    setCollapsed(c => {
      localStorage.setItem('morpho-sidebar-collapsed', String(!c))
      return !c
    })
  }

  const onDragStart = (e) => {
    e.preventDefault()
    dragging.current = true
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    const onMove = (ev) => {
      if (!dragging.current) return
      const w = Math.max(200, Math.min(480, ev.clientX))
      setSidebarWidth(w)
    }
    const onUp = () => {
      dragging.current = false
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
      localStorage.setItem('morpho-sidebar-width', String(sidebarWidth))
      document.removeEventListener('mousemove', onMove)
      document.removeEventListener('mouseup', onUp)
    }
    document.addEventListener('mousemove', onMove)
    document.addEventListener('mouseup', onUp)
  }

  // SSE events
  useSSE(useCallback((ev) => {
    dispatch({ type: 'SSE_EVENT', payload: ev })
  }, [dispatch]))

  // Load sessions on mount
  useEffect(() => {
    fetch('/api/sessions')
      .then(r => r.json())
      .then(data => dispatch({ type: 'SET_SESSIONS', payload: data }))
      .catch(() => {})

    fetch('/history')
      .then(r => r.json())
      .then(msgs => {
        if (Array.isArray(msgs) && msgs.length > 0) {
          dispatch({ type: 'SET_MESSAGES', payload: msgs })
        }
      })
      .catch(() => {})
  }, [dispatch])

  return (
    <>
      <Sidebar width={sidebarWidth} collapsed={collapsed} onToggle={toggleCollapsed} onDragStart={onDragStart} />
      <ChatArea />
    </>
  )
}

export default function App() {
  return (
    <StoreProvider>
      <AppInner />
    </StoreProvider>
  )
}
