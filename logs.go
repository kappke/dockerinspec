package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const maxLogLines = 4000

type logBatchMsg struct {
	gen   int
	lines []string
}

type logStream struct {
	id     string
	gen    int
	cancel context.CancelFunc
}

var prog *tea.Program

var ansiRe = regexp.MustCompile(`\x1b(\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_])`)

func cleanLine(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	if i := strings.LastIndexByte(s, '\r'); i >= 0 && i < len(s)-1 {
		s = s[i+1:]
	}
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
}

// startLogs follows `docker logs` for a container and delivers batches of
// lines to the program every ~100ms.
func startLogs(id string, gen int) *logStream {
	ctx, cancel := context.WithCancel(context.Background())
	ls := &logStream{id: id, gen: gen, cancel: cancel}
	go func() {
		cmd := exec.CommandContext(ctx, "docker", "logs", "--tail", "500", "-f", id)
		pr, pw, err := os.Pipe()
		if err != nil {
			return
		}
		cmd.Stdout, cmd.Stderr = pw, pw
		if err := cmd.Start(); err != nil {
			pw.Close()
			pr.Close()
			return
		}
		pw.Close()

		var mu sync.Mutex
		var pending []string
		done := make(chan struct{})
		flush := func() {
			mu.Lock()
			b := pending
			pending = nil
			mu.Unlock()
			if len(b) > 0 && prog != nil && ctx.Err() == nil {
				prog.Send(logBatchMsg{gen: gen, lines: b})
			}
		}
		go func() {
			t := time.NewTicker(100 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					flush()
				case <-done:
					flush()
					return
				}
			}
		}()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			l := cleanLine(sc.Text())
			mu.Lock()
			pending = append(pending, l)
			mu.Unlock()
		}
		close(done)
		pr.Close()
		cmd.Wait()
	}()
	return ls
}

func (l *logStream) stop() {
	if l != nil {
		l.cancel()
	}
}

var (
	reErr  = regexp.MustCompile(`(?i)\b(error|err|fatal|panic|exception|traceback|critical)\b`)
	reWarn = regexp.MustCompile(`(?i)\b(warn|warning)\b`)
	reDbg  = regexp.MustCompile(`(?i)\b(debug|trace)\b`)
)

func logStyle(l string) styleID {
	switch {
	case reErr.MatchString(l):
		return sty("203", "", false)
	case reWarn.MatchString(l):
		return sty("214", "", false)
	case reDbg.MatchString(l):
		return sty("244", "", false)
	}
	return sty("252", "", false)
}
