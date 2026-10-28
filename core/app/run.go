package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
	"github.com/suakitsu/picpilot-platform/core/server"
	"github.com/suakitsu/picpilot-platform/core/store"
)

func Run(edition config.Edition, args []string, stdout, stderr io.Writer) int {
	return RunWithInput(edition, args, os.Stdin, stdout, stderr)
}

func RunWithInput(edition config.Edition, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("picpilot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	showVersion := flags.Bool("version", false, "print edition and version")
	migrate := flags.Bool("migrate", false, "apply database migrations and exit")
	setupToken := flags.Bool("setup-token", false, "explicitly print the local first-run setup token")
	healthcheck := flags.Bool("healthcheck", false, "check the running local HTTP readiness endpoint")
	resetOwner := flags.Bool("reset-owner-password-stdin", false, "reset a privileged owner's password from standard input and revoke all sessions")
	account := flags.String("account", "", "owner email for explicit local recovery")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *resetOwner && (*account == "" || *showVersion || *migrate || *setupToken) || (!*resetOwner && *account != "") {
		fmt.Fprintln(stderr, "Owner recovery requires --reset-owner-password-stdin and --account only.")
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "PicPilot %s Go development\n", edition)
		return 0
	}
	if *healthcheck {
		return checkHealth()
	}
	c, err := config.Load(edition)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *setupToken {
		fmt.Fprintln(stdout, c.SetupToken)
		return 0
	}
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 60*time.Second)
	db, err := store.Open(startup, c.DatabaseURL, edition)
	cancel()
	if err != nil {
		logger.Error("database initialization failed", "sqlstate", store.SQLState(err))
		return 1
	}
	defer db.Close()
	if *resetOwner {
		password, err := readRecoveryPassword(stdin)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		hash, err := security.HashPassword(password)
		if err != nil {
			fmt.Fprintln(stderr, "Password must contain 12-72 UTF-8 bytes.")
			return 2
		}
		recoverContext, recoverCancel := context.WithTimeout(ctx, 15*time.Second)
		err = db.RecoverOwnerPassword(recoverContext, *account, hash)
		recoverCancel()
		if err != nil {
			fmt.Fprintln(stderr, "Owner recovery failed; verify the account and database connection.")
			return 1
		}
		fmt.Fprintln(stdout, "Owner password changed. All previous sessions are revoked; sign in again.")
		return 0
	}
	if *migrate {
		fmt.Fprintln(stdout, "Database migration and edition checks passed.")
		return 0
	}
	api, err := server.New(c, db, logger)
	if err != nil {
		logger.Error("server initialization failed")
		return 1
	}
	defer api.Files.Close()
	jobsContext, stopJobs := context.WithCancel(ctx)
	jobsDone := make(chan struct{})
	var jobsErr error
	go func() { jobsErr = api.RunBackground(jobsContext); close(jobsDone) }()
	defer func() {
		stopJobs()
		<-jobsDone
		if jobsErr != nil {
			logger.Error("task runner stopped with an error")
		}
	}()
	httpServer := &http.Server{Addr: c.Address, Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
	errorsCh := make(chan error, 1)
	go func() {
		logger.Info("PicPilot listening", "edition", edition, "address", c.Address)
		errorsCh <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-errorsCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP listener failed")
			return 1
		}
	case <-ctx.Done():
	case <-jobsDone:
		logger.Error("background service stopped unexpectedly")
		_ = httpServer.Close()
		return 1
	}
	shutdown, shutdownCancel := context.WithTimeout(context.Background(), c.ShutdownTimeout)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdown); err != nil {
		_ = httpServer.Close()
		logger.Error("HTTP shutdown deadline exceeded")
		return 1
	}
	return 0
}

func checkHealth() int {
	address := os.Getenv("PICPILOT_LISTEN")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return 1
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://127.0.0.1:" + port + "/api/health/ready")
	if err != nil {
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func readRecoveryPassword(input io.Reader) (string, error) {
	if input == nil {
		return "", errors.New("Password must be supplied through standard input.")
	}
	data, err := io.ReadAll(io.LimitReader(input, 1025))
	if err != nil || len(data) > 1024 {
		return "", errors.New("Cannot read password from standard input.")
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if strings.ContainsAny(password, "\r\n\x00") || len(password) < 12 || len(password) > 72 {
		return "", errors.New("Password must contain 12-72 UTF-8 bytes on one line.")
	}
	return password, nil
}
