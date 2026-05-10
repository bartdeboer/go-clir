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

	r.Group("help", "Root help.")
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

func TestRouter_Resolve_ExactExecutableHelpRouteWinsNaturally(t *testing.T) {
	r := New()

	r.Group("component <component>", "Component root")
	r.Handle("component <component> help", "Dynamic component help", func(req *Request) error { return nil })

	commandRes, err := r.Resolve(context.Background(), []string{"component", "api", "help"}, ResolveHelp())
	if err != nil {
		t.Fatalf("Resolve command returned error: %v", err)
	}
	if commandRes.Kind != ResolutionCommand {
		t.Fatalf("Kind = %q, want %q", commandRes.Kind, ResolutionCommand)
	}
	if commandRes.Request == nil {
		t.Fatal("command resolution missing Request")
	}
	if len(commandRes.Request.Extra) != 0 {
		t.Fatalf("Extra = %v, want empty", commandRes.Request.Extra)
	}
	if got := commandRes.Request.Params["component"]; got != "api" {
		t.Fatalf("component param = %q, want %q", got, "api")
	}
}

func TestRouter_Resolve_HandlerlessHelpRouteResolvesAsContextualHelp(t *testing.T) {
	r := New()

	r.Group("component <component>", "Component root")
	r.Group("component <component> help", "Component help metadata")

	res, err := r.Resolve(context.Background(), []string{"component", "api", "help"}, ResolveHelp())
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if res.Kind != ResolutionHelp {
		t.Fatalf("Kind = %q, want %q", res.Kind, ResolutionHelp)
	}
	if strings.Join(res.HelpScope, " ") != "component api" {
		t.Fatalf("HelpScope = %v, want [component api]", res.HelpScope)
	}
}

func TestRouter_Run_GroupRouteDoesNotExecute(t *testing.T) {
	r := New()

	r.Group("component <component>", "Component root")

	err := r.Run(context.Background(), []string{"component", "api"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRouter_FRunWithHelp_GroupRouteAppearsInContextualHelp(t *testing.T) {
	r := New()

	r.Group("help", "Root help.")
	r.Group("component", "Component commands")

	var buf bytes.Buffer
	if err := r.FRunWithHelp(context.Background(), &buf, []string{"help"}); err != nil {
		t.Fatalf("FRunWithHelp returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "component") {
		t.Fatalf("missing group route from help: %q", out)
	}
	if !strings.Contains(out, "Component commands") {
		t.Fatalf("missing group route description from help: %q", out)
	}
}

func TestRouter_FRunWithHelp_GroupRouteContributesIntro(t *testing.T) {
	r := New()

	r.Group("component <component>", "Manage one component.")
	r.Handle("component <component> status", "Show status", func(req *Request) error { return nil })

	var buf bytes.Buffer
	if err := r.FRunWithHelp(context.Background(), &buf, []string{"component", "api", "help"}); err != nil {
		t.Fatalf("FRunWithHelp returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Manage one component.") {
		t.Fatalf("missing group intro: %q", out)
	}
	if !strings.Contains(out, "component <component> status") {
		t.Fatalf("missing child command: %q", out)
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
