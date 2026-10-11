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
	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/mangle"
)

// credentialEnvVars are the variables browser-test fixtures resolve through
// value_env. The doctor reports whether each is set, never its value or length:
// an unset one makes a login fixture submit an empty password and stay on the
// login page, which reads as a wrong password.
var credentialEnvVars = []string{"BROWSERNERD_TEST_PASSWORD"}

// DoctorDeps is what the self-diagnosis inspects. Any field may be nil.
type DoctorDeps struct {
	Config   config.Config
	Sessions *browser.SessionManager
	Engine   *mangle.Engine
	Docker   *docker.Client
	Calls    *callTracker
}

// DoctorReport is BrowserNERD's self-diagnosis: which binary is running, the
// configuration it resolved, and whether Chrome, the fact engine, Docker log
// correlation and in-flight calls are healthy. It needs no session, so it
// answers while every tab is wedged. problems lists what needs fixing, first.
func DoctorReport(ctx context.Context, d DoctorDeps) map[string]interface{} {
	problems := make([]string, 0)
	report := map[string]interface{}{}

	info := buildinfo.Read()
	binary := map[string]interface{}{
		"version": info.Version,
		"built":   info.BuildTime.Format(time.RFC3339),
	}
	if info.Revision != "" {
		binary["revision"] = info.Revision
		if info.Modified {
			binary["revision"] = info.Revision + "+dirty"
		}
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

	report["config"] = map[string]interface{}{
		"chrome":           doctorChrome(cfg.Browser),
		"headless":         cfg.Browser.IsHeadless(),
		"progressive_only": cfg.MCP.IsProgressiveOnly(),
		"tool_timeout":     cfg.MCP.GetToolTimeout().String(),
		"dialog_policy":    cfg.Browser.GetDialogPolicy(),
		"fact_buffer":      cfg.Mangle.FactBufferLimit,
		"docker":           cfg.Docker.Enabled,
	}

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
		browserInfo := map[string]interface{}{
			"connected": d.Sessions.IsConnected(),
			"active":    active,
			"detached":  detached,
		}
		if len(activeRows) > 0 {
			browserInfo["sessions"] = activeRows
		}
		report["browser"] = browserInfo
	}

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

	if cfg.Docker.Enabled {
		dockerInfo := map[string]interface{}{"containers": cfg.Docker.Containers}
		if d.Docker != nil {
			probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			_, err := d.Docker.QueryLogs(probeCtx, time.Now())
			cancel()
			if err != nil {
				dockerInfo["reachable"] = false
				dockerInfo["error"] = clipCallText(err.Error(), 160)
				problems = append(problems, "Docker logs unreachable: backend correlation is off until Docker is running")
			} else {
				dockerInfo["reachable"] = true
			}
		}
		report["docker"] = dockerInfo
	}

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
		Docker:   s.dockerClient,
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
