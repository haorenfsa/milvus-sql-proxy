package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/flike/kingshard/core/golog"
	"github.com/haorenfsa/milvus-sql-proxy/pkg"
)

var BuildDate, BuildVersion string

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("milvus-sql-proxy", flag.ContinueOnError)
	flags.SetOutput(out)
	config := flags.String("config", "./config.yaml", "configuration file")
	logLevel := flags.String("log-level", "", "override log level: debug, info, warn, error")
	version := flags.Bool("v", false, "print build version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *version {
		fmt.Fprintf(out, "milvus-sql-proxy %s (%s)\n", BuildVersion, BuildDate)
		return nil
	}
	cfg, err := pkg.ParseConfigFile(*config)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	level := cfg.LogLevel
	if *logLevel != "" {
		level = *logLevel
	}
	setLogLevel(level)
	if cfg.LogPath != "" {
		h, err := golog.NewRotatingFileHandler(filepath.Join(cfg.LogPath, "sys.log"), 1<<30, 1)
		if err != nil {
			return err
		}
		old := golog.GlobalSysLogger
		golog.GlobalSysLogger = golog.New(h, golog.Lfile|golog.Ltime|golog.Llevel)
		defer func() { golog.GlobalSysLogger.Close(); golog.GlobalSysLogger = old }()
		setLogLevel(level)
	}
	s, err := pkg.NewServer(cfg)
	if err != nil {
		return err
	}
	defer s.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			s.Close()
		case <-done:
		}
	}()
	return s.Run()
}
func setLogLevel(level string) {
	switch strings.ToLower(level) {
	case "debug":
		golog.GlobalSysLogger.SetLevel(golog.LevelDebug)
	case "warn":
		golog.GlobalSysLogger.SetLevel(golog.LevelWarn)
	case "error":
		golog.GlobalSysLogger.SetLevel(golog.LevelError)
	default:
		golog.GlobalSysLogger.SetLevel(golog.LevelInfo)
	}
}
