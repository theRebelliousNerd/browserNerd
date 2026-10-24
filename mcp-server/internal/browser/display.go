package browser

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type displayProcess struct {
	cmd  *exec.Cmd
	done chan error
}

func startDisplayProcess(name string, args []string, env []string, logFile *os.File) (*displayProcess, error) {
	cmd := exec.Command(name, args...)
	cmd.Env, cmd.Stdout, cmd.Stderr = env, logFile, logFile
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &displayProcess{cmd: cmd, done: make(chan error, 1)}
	go func() { p.done <- cmd.Wait(); close(p.done) }()
	return p, nil
}

func (p *displayProcess) stop() error {
	if p == nil {
		return nil
	}
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(2 * time.Second):
	}
	if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("display process %d did not exit", p.cmd.Process.Pid)
	}
}

// managedDisplay belongs to one managed Chrome. Its environment is passed only
// to children, never written to the server's or user's process environment.
type managedDisplay struct {
	name        string
	env         []string
	xserver, wm *displayProcess
	dir         string
	log         *os.File
	once        sync.Once
	closeErr    error
}

func (d *managedDisplay) Close() error {
	if d == nil {
		return nil
	}
	d.once.Do(func() {
		d.closeErr = errors.Join(d.wm.stop(), d.xserver.stop(), d.log.Close())
		if d.xserver != nil && d.xserver.cmd.ProcessState != nil {
			number := strings.TrimPrefix(d.name, ":")
			lock := "/tmp/.X" + number + "-lock"
			owner, err := os.ReadFile(lock)
			if err == nil && strings.TrimSpace(string(owner)) == strconv.Itoa(d.xserver.cmd.Process.Pid) {
				// A forced kill cannot let Xvfb clean its own socket and lock.
				for _, path := range []string{"/tmp/.X11-unix/X" + number, lock} {
					if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
						d.closeErr = errors.Join(d.closeErr, err)
					}
				}
			}
		}
		d.closeErr = errors.Join(d.closeErr, os.RemoveAll(d.dir))
	})
	return d.closeErr
}

func privateDisplayEnvironment(ambient []string, display string) []string {
	env := make([]string, 0, len(ambient)+5)
	for _, entry := range ambient {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "DISPLAY", "WAYLAND_DISPLAY", "XDG_SESSION_TYPE", "XAUTHORITY", "DBUS_SESSION_BUS_ADDRESS":
		default:
			env = append(env, entry)
		}
	}
	return append(env, "DISPLAY="+display, "WAYLAND_DISPLAY=", "XDG_SESSION_TYPE=x11", "XAUTHORITY=", "DBUS_SESSION_BUS_ADDRESS=")
}

func startManagedDisplay(width, height int) (_ *managedDisplay, err error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("private headed display backend is unavailable on %s", runtime.GOOS)
	}
	for _, name := range []string{"Xvfb", "openbox"} {
		if _, err := exec.LookPath(name); err != nil {
			return nil, fmt.Errorf("browser.isolated_display requires %s on PATH: %w", name, err)
		}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(40000))
	if err != nil {
		return nil, err
	}
	number := int(n.Int64()) + 10000
	name := fmt.Sprintf(":%d", number)
	lock := fmt.Sprintf("/tmp/.X%d-lock", number)
	socket := fmt.Sprintf("/tmp/.X11-unix/X%d", number)
	for _, path := range []string{lock, socket} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return nil, fmt.Errorf("refusing occupied or unverifiable private display %s", name)
		}
	}
	dir, err := os.MkdirTemp("", "browsernerd-display-")
	if err != nil {
		return nil, err
	}
	d := &managedDisplay{name: name, dir: dir, env: privateDisplayEnvironment(os.Environ(), name)}
	d.log, err = os.OpenFile(filepath.Join(dir, "display.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, d.Close())
		}
	}()
	if width < 1280 {
		width = 1280
	}
	if height < 1024 {
		height = 1024
	}
	d.xserver, err = startDisplayProcess("Xvfb", []string{name, "-screen", "0", fmt.Sprintf("%dx%dx24", width, height), "-nolisten", "tcp", "-ac"}, d.env, d.log)
	if err != nil {
		return nil, fmt.Errorf("start private X server: %w", err)
	}
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		select {
		case exitErr := <-d.xserver.done:
			return nil, fmt.Errorf("private X server exited before readiness: %v", exitErr)
		default:
		}
		owner, readErr := os.ReadFile(lock)
		if readErr == nil && strings.TrimSpace(string(owner)) == strconv.Itoa(d.xserver.cmd.Process.Pid) {
			if info, statErr := os.Lstat(socket); statErr == nil && info.Mode()&os.ModeSocket != 0 {
				ready = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		return nil, fmt.Errorf("private display %s failed ownership/socket readiness", name)
	}
	d.wm, err = startDisplayProcess("openbox", []string{"--sm-disable"}, d.env, d.log)
	if err != nil {
		return nil, fmt.Errorf("start private window manager: %w", err)
	}
	select {
	case exitErr := <-d.wm.done:
		return nil, fmt.Errorf("private window manager exited: %v", exitErr)
	case <-time.After(100 * time.Millisecond):
	}
	return d, nil
}
