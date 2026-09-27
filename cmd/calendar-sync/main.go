// Command calendar-sync mirrors source Google Calendars onto a hub calendar.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"google.golang.org/api/calendar/v3"

	"github.com/codeallthethingz/calendar-sync/internal/auth"
	"github.com/codeallthethingz/calendar-sync/internal/config"
	"github.com/codeallthethingz/calendar-sync/internal/gcal"
	"github.com/codeallthethingz/calendar-sync/internal/state"
	calsync "github.com/codeallthethingz/calendar-sync/internal/sync"
)

const (
	envClientID     = "GOOGLE_CLIENT_ID"
	envClientSecret = "GOOGLE_CLIENT_SECRET"
	envRefreshToken = "GOOGLE_REFRESH_TOKEN"
)

const usage = `usage: calendar-sync <command> [flags]

commands:
  sync       run one sync pass (--config config.yaml --state state.json)
  auth       run the one-time OAuth consent flow and print a refresh token
  calendars  list calendars visible to the hub account

environment:
  GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET        all commands
  GOOGLE_REFRESH_TOKEN                          sync and calendars
  HUB_CALENDAR_ID, CALENDAR_ID_<SOURCE NAME>    sync
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var err error
	switch args[0] {
	case "sync":
		err = runSync(ctx, args[1:])
	case "auth":
		err = runAuth(ctx, args[1:])
	case "calendars":
		err = runCalendars(ctx, args[1:])
	case "-h", "--help", "help":
		fmt.Fprint(os.Stdout, usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "calendar-sync %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func runSync(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	configPath := fs.String("config", "config.yaml", "path to config file")
	statePath := fs.String("state", "state.json", "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath, os.Getenv)
	if err != nil {
		return err
	}
	st, err := state.Load(*statePath)
	if err != nil {
		return err
	}
	api, err := newService(ctx)
	if err != nil {
		return err
	}
	s := &calsync.Syncer{API: api, Config: cfg, Now: time.Now, Log: log.New(os.Stderr, "", log.LstdFlags)}
	runErr := s.Run(ctx, st)
	if err := st.Save(*statePath); err != nil {
		return errors.Join(runErr, err)
	}
	return runErr
}

func runAuth(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("auth", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	env, err := requireEnv(envClientID, envClientSecret)
	if err != nil {
		return err
	}
	cfg := auth.Config(env[envClientID], env[envClientSecret])
	token, err := auth.Login(ctx, cfg, browserOpener(), os.Stderr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Refresh token below. Store it as the %s secret.\n", envRefreshToken)
	fmt.Fprintln(os.Stdout, token)
	return nil
}

func runCalendars(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("calendars", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	svc, err := newService(ctx)
	if err != nil {
		return err
	}
	entries, err := svc.CalendarList(ctx)
	if err != nil {
		return err
	}
	return printCalendars(os.Stdout, entries)
}

func printCalendars(w io.Writer, entries []*calendar.CalendarListEntry) error {
	for _, e := range entries {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", e.Id, e.Summary, e.AccessRole); err != nil {
			return fmt.Errorf("write calendar list: %w", err)
		}
	}
	return nil
}

func newService(ctx context.Context) (*gcal.Service, error) {
	env, err := requireEnv(envClientID, envClientSecret, envRefreshToken)
	if err != nil {
		return nil, err
	}
	cfg := auth.Config(env[envClientID], env[envClientSecret])
	return gcal.New(ctx, auth.HTTPClient(ctx, cfg, env[envRefreshToken]))
}

func requireEnv(names ...string) (map[string]string, error) {
	values := make(map[string]string, len(names))
	var missing []string
	for _, n := range names {
		v := os.Getenv(n)
		if v == "" {
			missing = append(missing, n)
		}
		values[n] = v
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing environment variables: %s", strings.Join(missing, ", "))
	}
	return values, nil
}

// browserOpener returns a function that opens a URL with `open` on macOS,
// or nil when that is not available.
func browserOpener() func(string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	path, err := exec.LookPath("open")
	if err != nil {
		return nil
	}
	return func(url string) error { return exec.Command(path, url).Run() }
}
