package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/buildinfo"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/logs"
	"browsernerd-mcp-server/internal/mangle"
)

// credentialEnvVars are the variables browser-test fixtures resolve through
// value_env. The doctor reports whether each is set, never its value or length:
// an unset one makes a login fixture submit an empty password and stay on the
// login page, which reads as a wrong password.
var credentialEnvVars = []string{"BROWSERNERD_TEST_PASSWORD"}

// DoctorDeps is what the self-diagnosis inspects. Any field may be nil.
type DoctorDeps struct {
	Config        config.Config
	Sessions      *browser.SessionManager
	Engine        *mangle.Engine
	Logs          logs.Source
	Calls         *callTracker
	ReapedOrphans int
}

func doctorBinaryIdentity(info buildinfo.Info) map[string]interface{} {
	binary := map[string]interface{}{
		"version": info.Version, "built": nil, "pid": info.PID,
		"identity_status": info.IdentityStatus, "identity_source": info.IdentitySource,
	}
	if !info.BuildTime.IsZero() {
		binary["built"] = info.BuildTime.Format(time.RFC3339)
	}
	if info.SHA256 != "" {
		binary["sha256"] = info.SHA256
	}
	if info.FileID != "" {
		binary["file_id"] = info.FileID
	}
	if info.IdentityError != "" {
		binary["identity_error"] = info.IdentityError
	}
	if info.Revision != "" {
		binary["revision"] = info.Revision
		if info.Modified {
			binary["revision"] = info.Revision + "+dirty"
		}
	}
	return binary
}

// DoctorReport is BrowserNERD's self-diagnosis: which binary is running, the
// configuration it resolved, and whether Chrome, the fact engine, server log
// sources and in-flight calls are healthy. It needs no session, so it
// answers while every tab is wedged. problems lists what needs fixing, first.
func DoctorReport(ctx context.Context, d DoctorDeps) map[string]interface{} {
	problems := make([]string, 0)
	report := map[string]interface{}{}

	info := buildinfo.Read()
	binary := doctorBinaryIdentity(info)
	if info.IdentityStatus != "verified" {
		problems = append(problems, "running binary identity unavailable: "+info.IdentityError)
	}
	if stale, at := buildinfo.StaleSource(buildinfo.SourceRootForBinary()); stale != "" {
		binary["stale"] = fmt.Sprintf("%s changed %s after this binary was built", filepath.Base(stale), at.Sub(info.BuildTime).Round(time.Second))
		problems = append(problems, "binary is older than its source: rebuild (go run ./cmd/rebuild) and reconnect the MCP server")
	}
	report["binary"] = binary

	cfg := d.Config
	paths := map[string]interface{}{}
	checkDir := func(name, dir string, mustWrite bool) {
		if dir == "" {
			return
		}
		st, err := os.Stat(dir)
		switch {
		case err != nil && os.IsNotExist(err) && mustWrite:
			// Created on first use; only its parent has to exist.
			if _, perr := os.Stat(filepath.Dir(dir)); perr != nil {
				paths[name] = dir + " (missing, parent missing)"
				problems = append(problems, fmt.Sprintf("%s directory %s cannot be created: its parent is missing", name, dir))
				return
			}
			paths[name] = dir + " (created on first use)"
		case err != nil:
			paths[name] = dir + " (" + err.Error() + ")"
			problems = append(problems, fmt.Sprintf("%s %s: %v", name, dir, err))
		case !st.IsDir():
			paths[name] = dir + " (not a directory)"
			problems = append(problems, fmt.Sprintf("%s %s is not a directory", name, dir))
		default:
			paths[name] = dir
		}
	}
	if cfg.Mangle.SchemaPath != "" {
		// The built-in schema ships in the binary; schema_path names project
		// modules added on top of it.
		if _, err := os.Stat(cfg.Mangle.SchemaPath); err != nil {
			paths["project_schema"] = cfg.Mangle.SchemaPath + " (missing)"
			problems = append(problems, "project Mangle modules not found at "+cfg.Mangle.SchemaPath)
		} else {
			paths["project_schema"] = cfg.Mangle.SchemaPath
		}
	}
	for i, root := range cfg.Security.WritableRoots {
		checkDir(fmt.Sprintf("writable_root_%d", i), root, true)
	}
	checkDir("downloads", cfg.Browser.GetDownloadDir(), true)
	if cfg.Server.LogFile != "" {
		paths["log_file"] = cfg.Server.LogFile
	}
	report["paths"] = paths

	// Variable names are listed as values, not used as keys: the server's
	// credential redactor blanks the value of any key that names a password.
	set, unset := make([]string, 0), make([]string, 0)
	for _, name := range credentialEnvVars {
		if os.Getenv(name) != "" {
			set = append(set, name)
		} else {
			unset = append(unset, name)
			problems = append(problems, name+" is unset in the server's environment: value_env logins will submit nothing (add it to the MCP server env and reconnect)")
		}
	}
	report["credentials"] = map[string]interface{}{"set": set, "unset": unset}

	displayBackend := config.DisplayBackend()
	configInfo := map[string]interface{}{
		"chrome":           doctorChrome(cfg.Browser),
		"headless":         cfg.Browser.IsHeadless(),
		"isolated_display": cfg.Browser.IsolatedDisplay,
		"display_backend":  displayBackend,
		"progressive_only": cfg.MCP.IsProgressiveOnly(),
		"tool_timeout":     cfg.MCP.GetToolTimeout().String(),
		"dialog_policy":    cfg.Browser.GetDialogPolicy(),
		"fact_buffer":      cfg.Mangle.FactBufferLimit,
		"docker":           cfg.Docker.Enabled,
	}
	if cfg.Browser.IsolatedDisplay && !displayBackend.Available {
		problems = append(problems, "requested isolated headed display backend is unavailable; install Xvfb and Openbox on Linux; no desktop fallback is allowed")
	}
	if len(cfg.Overrides) > 0 {
		configInfo["overrides"] = cfg.Overrides
		problems = append(problems, cfg.Overrides...)
	}
	report["config"] = configInfo

	reaped := d.ReapedOrphans
	if d.Sessions != nil {
		active, detached := 0, 0
		activeRows := make([]map[string]interface{}, 0)
		for _, s := range d.Sessions.List() {
			if s.Status == "detached" {
				detached++
				continue
			}
			active++
			if len(activeRows) < 8 {
				activeRows = append(activeRows, map[string]interface{}{"id": s.ID, "url": s.URL, "status": s.Status})
			}
		}
		if sReaped := d.Sessions.ReapedOrphans(); sReaped > reaped {
			reaped = sReaped
		}
		connection := d.Sessions.ConnectionHealth(ctx)
		browserInfo := map[string]interface{}{
			"connected":         connection.Connected,
			"connection_state":  connection.State,
			"connection_reason": connection.Reason,
			"isolated_display":  d.Config.Browser.IsolatedDisplay,
			"active":            active,
			"detached":          detached,
			"reaped_orphans":    reaped,
		}
		if connection.BrowserID != "" {
			browserInfo["default_browser_id"] = connection.BrowserID
		}
		if connection.Error != "" {
			browserInfo["connection_error"] = connection.Error
		}
		if !connection.Connected && connection.State != "not_started" {
			problems = append(problems, fmt.Sprintf("browser CDP transport %s (%s): %s", connection.State, connection.Reason, connection.Error))
		}
		if len(activeRows) > 0 {
			browserInfo["sessions"] = activeRows
		}
		report["browser"] = browserInfo
	}
	report["reaped_orphans"] = reaped

	if d.Engine != nil {
		st := d.Engine.Stats()
		engineInfo := map[string]interface{}{
			"ready":      st.Ready,
			"facts":      st.Facts,
			"limit":      st.Limit,
			"by_class":   st.FactsByClass,
			"evicted":    st.EvictedTotal,
			"rules":      st.Rules,
			"predicates": st.Predicates,
			"modules":    len(st.Modules),
		}
		project := make([]string, 0)
		for _, m := range st.Modules {
			if !strings.HasPrefix(m, "builtin:") {
				project = append(project, m)
			}
		}
		if len(project) > 0 {
			engineInfo["project_modules"] = project
		}
		if st.SamplingRate < 1 {
			engineInfo["sampling_rate"] = st.SamplingRate
			engineInfo["sampled_out"] = st.SampledOut
		}
		if st.LastEvalError != "" {
			engineInfo["last_eval_error"] = clipCallText(st.LastEvalError, 200)
			problems = append(problems, "the last Mangle evaluation failed: derived facts (failed_request, caused_by, ...) may be stale")
		}
		if hi := st.Evicted["high"]; hi > 0 {
			problems = append(problems, fmt.Sprintf("%d failure-evidence facts were evicted: raise mangle.fact_buffer_limit", hi))
		}
		report["engine"] = engineInfo
	}

	logSources := make([]map[string]interface{}, 0)
	for _, file := range cfg.Logs.Files {
		logSources = append(logSources, map[string]interface{}{"type": "file", "name": file.Name})
	}
	if cfg.Docker.Enabled {
		for _, name := range cfg.Docker.Containers {
			logSources = append(logSources, map[string]interface{}{"type": "docker", "name": name})
		}
	}
	logInfo := map[string]interface{}{"configured": len(logSources) > 0, "sources": logSources}
	if d.Logs != nil {
		probeCtx, cancel := context.WithTimeout(ctx, backendQueryTimeout)
		window := d.Logs.LogWindow()
		if window <= 0 {
			window = defaultBackendWindow
		}
		now := time.Now()
		entries, err := d.Logs.LogsBetween(probeCtx, now.Add(-window), now)
		cancel()
		logInfo["available_entries"] = len(entries)
		logInfo["complete"] = err == nil
		if err != nil {
			logInfo["error"] = backendLogError(err)
			problems = append(problems, "server log evidence incomplete: "+backendLogError(err))
		}
	} else if len(logSources) > 0 {
		logInfo["complete"] = false
		problems = append(problems, "configured server log sources are not connected")
	}
	report["logs"] = logInfo

	if d.Calls != nil {
		calls := d.Calls.report()
		report["calls"] = calls
		if inflight, ok := calls["in_flight"].([]map[string]interface{}); ok {
			for _, row := range inflight {
				if row["timed_out"] == true {
					problems = append(problems, fmt.Sprintf("%v %v is still running after its timeout (session %v): close-session and session_create a fresh tab", row["tool"], row["detail"], row["session_id"]))
				}
			}
		}
	}

	status := "ok"
	if len(problems) > 0 {
		status = "degraded"
	}
	report["status"] = status
	report["problems"] = problems
	return report
}

func (s *Server) doctorReport(ctx context.Context) map[string]interface{} {
	return DoctorReport(ctx, DoctorDeps{
		Config:   s.cfg,
		Sessions: s.sessions,
		Engine:   s.engine,
		Logs:     s.logSource,
		Calls:    s.calls,
	})
}

// doctorChrome names the browser the server attaches to or launches.
func doctorChrome(cfg config.BrowserConfig) string {
	if cfg.DebuggerURL != "" {
		return "attach to browser.debugger_url"
	}
	if bin := browser.ChromeBinary(cfg); bin != "" {
		return bin
	}
	return "none installed: Rod downloads Chromium on first launch"
}
