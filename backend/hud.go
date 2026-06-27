package backend

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/user/flow/backend/internal/speech"
)

// HUDServer is a tiny localhost HTTP server that backs the floating Quick
// Agent HUD. The HUD is a standalone WKWebView (it is NOT part of the Wails
// webview), so it cannot use Wails bindings/events. Instead it talks to Go
// over plain HTTP:
//
//	Go → JS : Server-Sent Events   (GET  /api/hud/events)
//	JS → Go : fetch POST           (POST /api/hud/ask, /approve, /open, …)
//
// The Svelte frontend (served from the embedded dist) renders the "#hud"
// route inside the WKWebView.
type HUDServer struct {
	app      *App
	baseURL  string // e.g. http://127.0.0.1:53017
	srv      *http.Server
	listener net.Listener

	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

// startHUDServer binds a localhost listener and serves the embedded frontend
// plus the HUD API. assets must be the frontend dist subtree (index.html at
// its root).
func (a *App) startHUDServer(assets fs.FS) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("hud listen: %w", err)
	}

	h := &HUDServer{
		app:      a,
		baseURL:  fmt.Sprintf("http://127.0.0.1:%d", ln.Addr().(*net.TCPAddr).Port),
		listener: ln,
		clients:  make(map[chan []byte]struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/hud/events", h.handleEvents)
	mux.HandleFunc("/api/hud/ask", h.handleAsk)
	mux.HandleFunc("/api/hud/approve", h.handleApprove)
	mux.HandleFunc("/api/hud/open", h.handleOpen)
	mux.HandleFunc("/api/hud/dismiss", h.handleDismiss)
	mux.HandleFunc("/api/hud/resize", h.handleResize)
	mux.Handle("/", http.FileServer(http.FS(assets)))

	h.srv = &http.Server{Handler: mux}
	a.hud = h

	go func() {
		if err := h.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[hud] server stopped: %v", err)
		}
	}()
	log.Printf("[hud] server listening at %s", h.baseURL)
	return nil
}

// hudURL returns the URL the native panel should load (the #hud route).
func (a *App) hudURL() string {
	if a.hud == nil {
		return ""
	}
	return a.hud.baseURL + "/hud.html"
}

// hudBroadcast pushes an event to every connected HUD SSE client.
func (a *App) hudBroadcast(event map[string]interface{}) {
	if a.hud == nil {
		return
	}
	a.hud.broadcast(event)
}

func (h *HUDServer) broadcast(event map[string]interface{}) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.Lock()
	for ch := range h.clients {
		select {
		case ch <- data:
		default: // drop if a slow client's buffer is full
		}
	}
	h.mu.Unlock()
}

// ─── HTTP handlers ───

func (h *HUDServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}()

	// Initial comment so the connection opens immediately.
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (h *HUDServer) handleAsk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.app.AskInQuickAsk(body.Text)
	w.WriteHeader(http.StatusNoContent)
}

func (h *HUDServer) handleApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID     string `json:"id"`
		Choice string `json:"choice"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.app.SubmitCommandApproval(body.ID, body.Choice)
	w.WriteHeader(http.StatusNoContent)
}

func (h *HUDServer) handleOpen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.app.OpenSessionInWindow(body.SessionID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *HUDServer) handleDismiss(w http.ResponseWriter, r *http.Request) {
	speech.HideHUD()
	w.WriteHeader(http.StatusNoContent)
}

func (h *HUDServer) handleResize(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Height int `json:"height"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	speech.ResizeHUD(body.Height)
	w.WriteHeader(http.StatusNoContent)
}
