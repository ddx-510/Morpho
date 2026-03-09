package ui

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/ddx-510/Morpho/chat"
	"github.com/ddx-510/Morpho/event"
)

//go:embed static
var staticFS embed.FS

type sseClient struct {
	ch chan []byte
}

type webState struct {
	mu      sync.Mutex
	clients map[*sseClient]bool
}

// ServeWeb starts the web UI HTTP server.
func ServeWeb(app *chat.App, port string) {
	ws := &webState{clients: make(map[*sseClient]bool)}

	// SSE broadcast to web clients
	app.Bus.OnAll(func(ev event.Event) {
		data, _ := json.Marshal(ev)
		ws.mu.Lock()
		for c := range ws.clients {
			select {
			case c.ch <- data:
			default:
			}
		}
		ws.mu.Unlock()
	})

	// Also log events to terminal so you can follow progress
	registerTerminalHooks(app)

	// Serve static files from the embedded filesystem.
	// SPA fallback: serve index.html for any path not matching a real file.
	staticSub, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(staticSub))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the exact file first (JS/CSS assets).
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		if _, err := fs.Stat(staticFS, "static"+path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback: serve index.html for client-side routing.
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "failed to load UI", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
	})

	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "SSE not supported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		client := &sseClient{ch: make(chan []byte, 64)}
		ws.mu.Lock()
		ws.clients[client] = true
		ws.mu.Unlock()

		defer func() {
			ws.mu.Lock()
			delete(ws.clients, client)
			ws.mu.Unlock()
		}()

		for {
			select {
			case data := <-client.ch:
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST only", 405)
			return
		}
		var req struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", 400)
			return
		}
		if req.Message == "" {
			http.Error(w, "empty message", 400)
			return
		}

		if strings.HasPrefix(req.Message, "/") {
			result := app.HandleCommand(req.Message)
			app.Bus.Emit(event.Event{Type: event.AssistantMessage, Content: result})
		} else {
			go func() {
				reply := app.HandleMessage(req.Message)
				app.Bus.Emit(event.Event{Type: event.AssistantMessage, Content: reply})
			}()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "processing"})
	})

	http.HandleFunc("/history", func(w http.ResponseWriter, r *http.Request) {
		h := app.History()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(h)
	})

	http.HandleFunc("/api/facts", func(w http.ResponseWriter, r *http.Request) {
		facts := app.Facts.All()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(facts)
	})

	http.HandleFunc("/api/workdir", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"dir": app.WorkDir})
	})

	http.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		sessions := app.Sessions.List()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"current":  app.SessionID(),
			"sessions": sessions,
		})
	})

	http.HandleFunc("/api/sessions/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST only", 405)
			return
		}
		id := app.NewSession()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	})

	http.HandleFunc("/api/sessions/load", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST only", 405)
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
			http.Error(w, "bad request", 400)
			return
		}
		if err := app.LoadSession(req.ID); err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		h := app.History()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":       req.ID,
			"messages": h,
		})
	})

	http.HandleFunc("/api/sessions/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST only", 405)
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
			http.Error(w, "bad request", 400)
			return
		}
		if err := app.Sessions.Delete(req.ID); err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	http.HandleFunc("/api/sessions/rename", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST only", 405)
			return
		}
		var req struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.Name == "" {
			http.Error(w, "bad request", 400)
			return
		}
		if err := app.Sessions.Rename(req.ID, req.Name); err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	fmt.Printf("\n  %s%s morpho %sweb UI at %shttp://localhost:%s%s\n\n",
		bold, peach, grayFg, lightGray, port, reset)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
