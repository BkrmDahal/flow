package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/user/flow/backend/internal/config"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ScheduledTask represents a recurring task that the scheduler will execute.
type ScheduledTask struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Instructions  string `json:"instructions"`
	CronExpr      string `json:"cronExpr"`                    // Standard 5-field cron: min hour dom month dow
	Enabled       bool   `json:"enabled"`
	LastRun       string `json:"lastRun,omitempty"`            // ISO 8601 timestamp
	LastSessionID string `json:"lastSessionID,omitempty"`      // Cowork session ID of the most recent run
	CreatedAt     string `json:"createdAt"`
}

// schedulesFile is the filename for persisted schedule data.
const schedulesFile = "schedules.json"

// schedSettingsFile stores schedule-level settings (catch-up toggle, etc.).
const schedSettingsFile = "schedule_settings.json"

// schedMu protects concurrent access to the schedules file.
var schedMu sync.Mutex

// ScheduleSettings holds global schedule preferences.
type ScheduleSettings struct {
	CatchUpMissed bool `json:"catchUpMissed"`
}

func loadScheduleSettings() ScheduleSettings {
	dir, err := config.FlowDir()
	if err != nil {
		return ScheduleSettings{}
	}
	data, err := os.ReadFile(filepath.Join(dir, schedSettingsFile))
	if err != nil {
		return ScheduleSettings{}
	}
	var s ScheduleSettings
	_ = json.Unmarshal(data, &s)
	return s
}

func saveScheduleSettings(s ScheduleSettings) error {
	dir, err := config.FlowDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, schedSettingsFile), data, 0o600)
}

// ── Persistence helpers ─────────────────────────────────────────────────────

func schedulesPath() (string, error) {
	dir, err := config.FlowDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, schedulesFile), nil
}

func loadSchedules() ([]ScheduledTask, error) {
	path, err := schedulesPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []ScheduledTask{}, nil
		}
		return nil, fmt.Errorf("read schedules: %w", err)
	}
	var tasks []ScheduledTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("unmarshal schedules: %w", err)
	}
	return tasks, nil
}

func saveSchedules(tasks []ScheduledTask) error {
	path, err := schedulesPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal schedules: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// ── Wails-bound CRUD ────────────────────────────────────────────────────────

// GetSchedules returns all scheduled tasks.
func (a *App) GetSchedules() ([]ScheduledTask, error) {
	schedMu.Lock()
	defer schedMu.Unlock()
	return loadSchedules()
}

// SaveSchedule creates or updates a scheduled task (upsert by ID).
func (a *App) SaveSchedule(task ScheduledTask) error {
	schedMu.Lock()
	defer schedMu.Unlock()

	tasks, err := loadSchedules()
	if err != nil {
		return err
	}

	if task.ID == "" {
		task.ID = fmt.Sprintf("sched_%d", time.Now().UnixMilli())
		task.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		task.Enabled = true
		tasks = append(tasks, task)
	} else {
		found := false
		for i, t := range tasks {
			if t.ID == task.ID {
				task.CreatedAt = t.CreatedAt
				task.LastRun = t.LastRun
				tasks[i] = task
				found = true
				break
			}
		}
		if !found {
			task.CreatedAt = time.Now().UTC().Format(time.RFC3339)
			tasks = append(tasks, task)
		}
	}

	return saveSchedules(tasks)
}

// DeleteSchedule removes a scheduled task by ID.
func (a *App) DeleteSchedule(id string) error {
	schedMu.Lock()
	defer schedMu.Unlock()

	tasks, err := loadSchedules()
	if err != nil {
		return err
	}

	filtered := make([]ScheduledTask, 0, len(tasks))
	for _, t := range tasks {
		if t.ID != id {
			filtered = append(filtered, t)
		}
	}

	return saveSchedules(filtered)
}

// ToggleSchedule flips the enabled state of a scheduled task.
func (a *App) ToggleSchedule(id string, enabled bool) error {
	schedMu.Lock()
	defer schedMu.Unlock()

	tasks, err := loadSchedules()
	if err != nil {
		return err
	}

	for i, t := range tasks {
		if t.ID == id {
			tasks[i].Enabled = enabled
			break
		}
	}

	return saveSchedules(tasks)
}

// GetScheduleCatchUp returns whether the "run missed tasks on startup" option is enabled.
func (a *App) GetScheduleCatchUp() bool {
	return loadScheduleSettings().CatchUpMissed
}

// SetScheduleCatchUp enables or disables the "run missed tasks on startup" option.
func (a *App) SetScheduleCatchUp(enabled bool) error {
	s := loadScheduleSettings()
	s.CatchUpMissed = enabled
	return saveScheduleSettings(s)
}

// RunScheduleNow manually triggers a scheduled task immediately.
// Returns the session ID of the created cowork session.
func (a *App) RunScheduleNow(id string) (string, error) {
	schedMu.Lock()
	tasks, err := loadSchedules()
	schedMu.Unlock()

	if err != nil {
		return "", err
	}

	for _, task := range tasks {
		if task.ID == id {
			log.Printf("flow: scheduler manual run task %q (%s)", task.Name, task.ID)
			sessionID := a.dispatchScheduledTask(task)

			// Update LastRun and LastSessionID.
			schedMu.Lock()
			freshTasks, err := loadSchedules()
			if err == nil {
				for j, ft := range freshTasks {
					if ft.ID == task.ID {
						freshTasks[j].LastRun = time.Now().UTC().Format(time.RFC3339)
						freshTasks[j].LastSessionID = sessionID
						break
					}
				}
				_ = saveSchedules(freshTasks)
			}
			schedMu.Unlock()
			return sessionID, nil
		}
	}

	return "", fmt.Errorf("schedule %q not found", id)
}

// ── Scheduler background loop ───────────────────────────────────────────────

// StartScheduler starts the background ticker that checks for due tasks.
func (a *App) StartScheduler() {
	ctx, cancel := context.WithCancel(context.Background())
	a.schedCancel = cancel

	go func() {
		// Run missed tasks on startup if enabled.
		a.runMissedSchedules()

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		log.Println("flow: scheduler started (30s tick)")
		for {
			select {
			case <-ctx.Done():
				log.Println("flow: scheduler stopped")
				return
			case <-ticker.C:
				a.checkDueTasks()
			}
		}
	}()
}

// runMissedSchedules checks if any enabled tasks missed their cron window
// while the app was closed, and dispatches them one by one.
func (a *App) runMissedSchedules() {
	settings := loadScheduleSettings()
	if !settings.CatchUpMissed {
		return
	}

	if a.llm == nil {
		log.Println("flow: scheduler catch-up skipped — no LLM configured")
		return
	}

	schedMu.Lock()
	tasks, err := loadSchedules()
	schedMu.Unlock()
	if err != nil {
		log.Printf("flow: scheduler catch-up load error: %v", err)
		return
	}

	now := time.Now()
	var missed []ScheduledTask

	for _, task := range tasks {
		if !task.Enabled {
			continue
		}
		if task.LastRun == "" {
			// Never run — treat as missed if the task was created > 2 min ago.
			created, err := time.Parse(time.RFC3339, task.CreatedAt)
			if err != nil || now.Sub(created) < 2*time.Minute {
				continue
			}
			missed = append(missed, task)
			continue
		}

		lastRun, err := time.Parse(time.RFC3339, task.LastRun)
		if err != nil {
			continue
		}

		if wasMissed(task.CronExpr, lastRun, now) {
			missed = append(missed, task)
		}
	}

	if len(missed) == 0 {
		log.Println("flow: scheduler catch-up — no missed tasks")
		return
	}

	log.Printf("flow: scheduler catch-up — %d missed task(s), running sequentially", len(missed))
	wailsRuntime.EventsEmit(a.ctx, "schedule:catchup:started", map[string]interface{}{
		"count": len(missed),
	})

	for _, task := range missed {
		log.Printf("flow: scheduler catch-up running %q (%s)", task.Name, task.ID)
		sessionID := a.dispatchScheduledTask(task)

		// Update LastRun and LastSessionID.
		schedMu.Lock()
		freshTasks, err := loadSchedules()
		if err == nil {
			for j, ft := range freshTasks {
				if ft.ID == task.ID {
					freshTasks[j].LastRun = now.UTC().Format(time.RFC3339)
					freshTasks[j].LastSessionID = sessionID
					break
				}
			}
			_ = saveSchedules(freshTasks)
		}
		schedMu.Unlock()
	}

	wailsRuntime.EventsEmit(a.ctx, "schedule:catchup:done", nil)
	log.Println("flow: scheduler catch-up complete")
}

// wasMissed checks whether at least one cron match occurred between lastRun and now.
// Walks minute-by-minute, capped at 24 hours lookback.
func wasMissed(cronExpr string, lastRun, now time.Time) bool {
	// Cap lookback to 24 hours.
	earliest := now.Add(-24 * time.Hour)
	if lastRun.Before(earliest) {
		lastRun = earliest
	}

	// Start checking from the minute after lastRun.
	check := lastRun.Truncate(time.Minute).Add(time.Minute)

	for check.Before(now) {
		if cronMatches(cronExpr, check) {
			return true
		}
		check = check.Add(time.Minute)
	}
	return false
}

// StopScheduler cancels the background scheduler.
func (a *App) StopScheduler() {
	if a.schedCancel != nil {
		a.schedCancel()
		a.schedCancel = nil
	}
}

// checkDueTasks evaluates all enabled tasks and dispatches any that are due.
func (a *App) checkDueTasks() {
	if a.llm == nil {
		return // No LLM configured — skip silently.
	}

	schedMu.Lock()
	tasks, err := loadSchedules()
	schedMu.Unlock()

	if err != nil {
		log.Printf("flow: scheduler load error: %v", err)
		return
	}

	now := time.Now()

	for i, task := range tasks {
		if !task.Enabled {
			continue
		}
		if !cronMatches(task.CronExpr, now) {
			continue
		}
		// Prevent re-triggering within the same minute.
		if task.LastRun != "" {
			lastRun, err := time.Parse(time.RFC3339, task.LastRun)
			if err == nil && now.Sub(lastRun) < 60*time.Second {
				continue
			}
		}

		log.Printf("flow: scheduler dispatching task %q (%s)", task.Name, task.ID)
		sessionID := a.dispatchScheduledTask(task)

		// Update LastRun and LastSessionID.
		schedMu.Lock()
		freshTasks, err := loadSchedules()
		if err == nil {
			for j, ft := range freshTasks {
				if ft.ID == task.ID {
					freshTasks[j].LastRun = now.UTC().Format(time.RFC3339)
					freshTasks[j].LastSessionID = sessionID
					break
				}
			}
			_ = saveSchedules(freshTasks)
		}
		schedMu.Unlock()

		// Update in-memory copy for the rest of this loop iteration.
		tasks[i].LastRun = now.UTC().Format(time.RFC3339)
	}
}

// ── Cron expression parser ──────────────────────────────────────────────────
// Supports standard 5-field cron: minute hour day-of-month month day-of-week
// Field values: number, *, */N, N-M, comma-separated lists

// cronMatches checks whether a 5-field cron expression matches the given time.
func cronMatches(expr string, t time.Time) bool {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return false
	}

	return cronFieldMatches(fields[0], t.Minute(), 0, 59) &&
		cronFieldMatches(fields[1], t.Hour(), 0, 23) &&
		cronFieldMatches(fields[2], t.Day(), 1, 31) &&
		cronFieldMatches(fields[3], int(t.Month()), 1, 12) &&
		cronFieldMatches(fields[4], int(t.Weekday()), 0, 6)
}

// cronFieldMatches checks whether a single cron field matches a value.
func cronFieldMatches(field string, value, min, max int) bool {
	// Handle comma-separated values: "1,5,10"
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if cronPartMatches(part, value, min, max) {
			return true
		}
	}
	return false
}

func cronPartMatches(part string, value, min, max int) bool {
	// Wildcard
	if part == "*" {
		return true
	}

	// Step: */N or N-M/S
	if strings.Contains(part, "/") {
		pieces := strings.SplitN(part, "/", 2)
		step, err := strconv.Atoi(pieces[1])
		if err != nil || step <= 0 {
			return false
		}
		rangeStr := pieces[0]

		rangeMin, rangeMax := min, max
		if rangeStr != "*" {
			if strings.Contains(rangeStr, "-") {
				rng := strings.SplitN(rangeStr, "-", 2)
				rangeMin, _ = strconv.Atoi(rng[0])
				rangeMax, _ = strconv.Atoi(rng[1])
			} else {
				rangeMin, _ = strconv.Atoi(rangeStr)
				rangeMax = max
			}
		}

		if value < rangeMin || value > rangeMax {
			return false
		}
		return (value-rangeMin)%step == 0
	}

	// Range: N-M
	if strings.Contains(part, "-") {
		rng := strings.SplitN(part, "-", 2)
		low, err1 := strconv.Atoi(rng[0])
		high, err2 := strconv.Atoi(rng[1])
		if err1 != nil || err2 != nil {
			return false
		}
		return value >= low && value <= high
	}

	// Exact number
	num, err := strconv.Atoi(part)
	if err != nil {
		return false
	}
	return value == num
}

// ── Dispatch ────────────────────────────────────────────────────────────────

// dispatchScheduledTask creates a new cowork session and runs the task.
// Returns the session ID of the created session.
func (a *App) dispatchScheduledTask(task ScheduledTask) string {
	sessionID := fmt.Sprintf("cowork_sched_%d", time.Now().UnixMilli())

	baseDir, err := config.FlowDir()
	if err != nil {
		log.Printf("flow: scheduler dispatch error: %v", err)
		return sessionID
	}
	workDir := filepath.Join(baseDir, "cowork", sessionID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		log.Printf("flow: scheduler mkdir error: %v", err)
		return sessionID
	}

	// Save session title.
	coworkDir := filepath.Join(baseDir, "cowork")
	titlesPath := filepath.Join(coworkDir, "session_titles.json")
	titles := make(map[string]string)
	if data, err := os.ReadFile(titlesPath); err == nil {
		_ = json.Unmarshal(data, &titles)
	}
	titles[sessionID] = task.Name
	if data, err := json.MarshalIndent(titles, "", "  "); err == nil {
		_ = os.WriteFile(titlesPath, data, 0o644)
	}

	content, err := json.Marshal(task.Instructions)
	if err != nil {
		log.Printf("flow: scheduler marshal error: %v", err)
		return sessionID
	}

	// Notify frontend that a scheduled task started.
	wailsRuntime.EventsEmit(a.ctx, "schedule:task:started", map[string]interface{}{
		"session_id": sessionID,
		"task_name":  task.Name,
		"task_id":    task.ID,
	})

	go a.runCoworkStream(sessionID, content, workDir, nil)
	return sessionID
}
