import { useEffect, useRef } from 'react'

export function useSSE(onEvent) {
  const cbRef = useRef(onEvent)
  cbRef.current = onEvent

  useEffect(() => {
    const es = new EventSource('/events')
    es.onmessage = (e) => {
      try {
        const data = JSON.parse(e.data)
        cbRef.current(data)
      } catch {}
    }
    return () => es.close()
  }, [])
}
