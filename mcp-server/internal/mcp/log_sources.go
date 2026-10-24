package mcp

import (
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/logs"
)

// NewLogSources constructs the shared evidence source used by MCP and the
// standalone doctor from validated configuration. Docker remains separately
// identifiable for predicates whose arguments explicitly name containers.
func NewLogSources(cfg config.Config) (logs.Source, *docker.Client) {
	var sources []logs.Source
	if len(cfg.Logs.Files) > 0 {
		files := make([]logs.File, 0, len(cfg.Logs.Files))
		for _, file := range cfg.Logs.Files {
			files = append(files, logs.File{Name: file.Name, Path: file.Path, AllowedRoot: file.AllowedRoot})
		}
		sources = append(sources, logs.NewFiles(files, cfg.Logs.GetLogWindow(), cfg.Logs.MaxReadBytes, cfg.Logs.MaxEntries))
	}
	var client *docker.Client
	if cfg.Docker.Enabled {
		client = docker.NewClient(cfg.Docker.Containers, cfg.Docker.GetLogWindow(), cfg.Docker.Host)
		sources = append(sources, client)
	}
	if len(sources) == 0 {
		return nil, client
	}
	window := sources[0].LogWindow()
	for _, source := range sources {
		if source.LogWindow() > window {
			window = source.LogWindow()
		}
	}
	return logs.NewComposite(sources, window), client
}
