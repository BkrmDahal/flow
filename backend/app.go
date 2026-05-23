package backend

import (
	"context"
	"log"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/user/flow/backend/internal/config"
	"github.com/user/flow/backend/internal/llamacpp"
	"github.com/user/flow/backend/internal/llm"
	"github.com/user/flow/backend/internal/session"
	"github.com/user/flow/backend/internal/speech"
	"github.com/user/flow/backend/internal/tools"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the root struct bound to the Wails frontend. All exported methods
// on App become callable from JavaScript.
type App struct {
	ctx     context.Context
	baseDir string
	cfg     *config.Config
	llm     llm.LLMClient
	llama   *llamacpp.Manager

	// Agent / session infrastructure
	sessionMgr *session.Manager
	tools      *tools.Registry

	voiceMu            sync.Mutex
	voiceRecordingPath string
}

// NewApp constructs the app instance. Heavy initialisation lives in Startup
// so it can use the Wails context.
func NewApp() *App {
	return &App{}
}

// Startup is invoked by Wails after the window is ready.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx

	base, err := config.Bootstrap()
	if err != nil {
		log.Printf("flow: config bootstrap failed: %v", err)
		return
	}
	a.baseDir = base

	cfg, err := config.Load(base)
	if err != nil {
		log.Printf("flow: config load failed: %v", err)
		return
	}
	a.cfg = cfg
	a.llama = llamacpp.NewManager(base)

	// Initialise session manager.
	a.sessionMgr = session.NewManager(base)

	// Initialise tool registry with full standard tools (memory, todo, skill, etc.)
	a.tools = tools.NewRegistry()
	tools.RegisterStandardTools(a.tools, base)

	if err := a.rebuildLLMClient(); err != nil {
		// Expected on first run before the user has configured a model.
		log.Printf("flow: LLM client not ready yet: %v", err)
	}

	// Show the Flow pinwheel icon in the macOS menu bar.
	speech.ShowMenuBarIcon()

	// Forward whisper.cpp model-download progress to the frontend.
	speech.SetLocalProgressCallback(func(stage string, downloaded, total int64) {
		wailsRuntime.EventsEmit(a.ctx, "flow:model:download:progress", map[string]interface{}{
			"stage":      stage,
			"downloaded": downloaded,
			"total":      total,
		})
	})

	// Set up dictation hotkey if enabled.
	a.setupDictationIfEnabled()

	log.Printf("flow: startup ok (base=%s, model=%q)", base, cfg.Model)
}

// Shutdown is invoked by Wails just before the window closes.
func (a *App) Shutdown(ctx context.Context) {
	if speech.IsDictationEnabled() {
		speech.TeardownDictation()
	}
	if a.llama != nil {
		if err := a.llama.Stop(); err != nil {
			log.Printf("flow: stop llama-server failed: %v", err)
		}
	}
	speech.HideMenuBarIcon()
	log.Println("flow: shutdown")
}

// Ping is a smoke-test method the frontend can call to verify Wails wiring.
func (a *App) Ping() string {
	return "pong"
}

// OpenLogFile opens ~/.flow/flow.log in the default macOS handler.
func (a *App) OpenLogFile() error {
	base, err := config.FlowDir()
	if err != nil {
		return err
	}
	logPath := filepath.Join(base, "flow.log")
	return exec.Command("open", logPath).Start()
}

// GetContext returns the Wails context, used by main.go menu callbacks.
func (a *App) GetContext() context.Context {
	return a.ctx
}

// SubmitSandboxApproval is called by the Svelte frontend to approve or deny a folder access request
func (a *App) SubmitSandboxApproval(id string, approved bool) {
	tools.SandboxApprovalMu.Lock()
	ch, exists := tools.PendingSandboxApprovals[id]
	tools.SandboxApprovalMu.Unlock()

	if exists {
		select {
		case ch <- approved:
		default:
		}
	}
}
