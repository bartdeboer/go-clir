package clir

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRouter_Run_UsesResolve(t *testing.T) {
	r := New()

	var called bool
	r.Handle("status", "Show status", func(req *Request) error {
		called = true
		return nil
	})

	if err := r.Run(context.Background(), []string{"status"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !called {
		t.Fatal("expected command handler to run")
	}
}

func TestRouter_FRunWithHelp_UsesResolve(t *testing.T) {
	r := New()

	r.Handle("help", "Root help.", func(req *Request) error { return nil })
	r.Handle("status", "Show status", func(req *Request) error { return nil })

	var buf bytes.Buffer
	if err := r.FRunWithHelp(context.Background(), &buf, []string{"help"}); err != nil {
		t.Fatalf("FRunWithHelp returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Root help.") {
		t.Fatalf("missing help intro: %q", out)
	}
	if !strings.Contains(out, "status") {
		t.Fatalf("missing help command: %q", out)
	}
}

func TestRouter_Resolve_CommandResult(t *testing.T) {
	r := New()

	r.Handle("component <component> status", "Show component status", func(req *Request) error { return nil })

	res, err := r.Resolve(context.Background(), []string{"component", "api", "status"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if res.Kind != ResolutionCommand {
		t.Fatalf("Kind = %q, want %q", res.Kind, ResolutionCommand)
	}
	if res.Request == nil {
		t.Fatal("command resolution missing Request")
	}
	if res.Handler == nil {
		t.Fatal("command resolution missing Handler")
	}
	if got := res.Request.Params["component"]; got != "api" {
		t.Fatalf("component param = %q, want %q", got, "api")
	}
}

func TestRouter_Resolve_HelpResult(t *testing.T) {
	r := New()

	res, err := r.Resolve(context.Background(), []string{"component", "api", "help", "all"}, ResolveHelp())
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if res.Kind != ResolutionHelp {
		t.Fatalf("Kind = %q, want %q", res.Kind, ResolutionHelp)
	}
	if strings.Join(res.HelpScope, " ") != "component api" {
		t.Fatalf("HelpScope = %v, want [component api]", res.HelpScope)
	}
	if !res.HelpAll {
		t.Fatal("HelpAll = false, want true")
	}
}

func TestRouter_Resolve_CanPreserveExactExecutableHelpRoute(t *testing.T) {
	r := New()

	r.Handle("component <component> help", "Dynamic component help", func(req *Request) error { return nil })

	helpRes, err := r.Resolve(context.Background(), []string{"component", "api", "help"}, ResolveHelp())
	if err != nil {
		t.Fatalf("Resolve help returned error: %v", err)
	}
	if helpRes.Kind != ResolutionHelp {
		t.Fatalf("Kind = %q, want %q", helpRes.Kind, ResolutionHelp)
	}

	commandRes, err := r.Resolve(
		context.Background(),
		[]string{"component", "api", "help"},
		ResolveHelp(),
		PreserveHelpCommand(),
	)
	if err != nil {
		t.Fatalf("Resolve command returned error: %v", err)
	}
	if commandRes.Kind != ResolutionCommand {
		t.Fatalf("Kind = %q, want %q", commandRes.Kind, ResolutionCommand)
	}
	if got := commandRes.Request.Params["component"]; got != "api" {
		t.Fatalf("component param = %q, want %q", got, "api")
	}
}

func TestRouter_FRunWithHelp_PrefersLiteralScopeOverParameterizedSibling(t *testing.T) {
	r := New()
	noop := func(req *Request) error { return nil }

	r.Handle("thread <thread> status", "Show thread status", noop)
	r.Handle("thread <thread> purge", "Purge thread", noop)
	r.Handle("thread current status", "Show current thread status", noop)
	r.Handle("thread current refresh", "Refresh current thread", noop)

	var buf bytes.Buffer
	if err := r.FRunWithHelp(context.Background(), &buf, []string{"thread", "current", "help"}); err != nil {
		t.Fatalf("FRunWithHelp returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "thread current status") {
		t.Fatalf("missing literal child route: %q", out)
	}
	if !strings.Contains(out, "thread current refresh") {
		t.Fatalf("missing literal sibling route: %q", out)
	}
	if strings.Contains(out, "thread <thread>") {
		t.Fatalf("parameterized sibling should not appear for literal scope: %q", out)
	}
}
