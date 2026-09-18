package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRun(t *testing.T) {
	var out bytes.Buffer
	if e := run(context.Background(), []string{"-v"}, &out); e != nil || out.Len() == 0 {
		t.Fatal(e)
	}
	if e := run(context.Background(), []string{"-bogus"}, &out); e == nil {
		t.Fatal("bad flags accepted")
	}
	if e := run(context.Background(), []string{"-config", "missing"}, &out); e == nil {
		t.Fatal("bad config accepted")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "config.yaml")
	if e := os.WriteFile(config, []byte("addr: 127.0.0.1:0\nlogPath: "+dir+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := run(ctx, []string{"-config", config, "-log-level", "debug"}, &out); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(config, []byte("addr: invalid-address\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := run(ctx, []string{"-config", config}, &out); e == nil {
		t.Fatal("invalid listener accepted")
	}
	for _, level := range []string{"debug", "warn", "error", "info", ""} {
		setLogLevel(level)
	}
}
