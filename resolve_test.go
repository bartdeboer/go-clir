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

func TestRouter_FPrintHelp_PrintsRootScope(t *testing.T) {
	r := New()

	r.Describe("", "Root help.")
	r.Handle("status", "Show status", func(req *Request) error { return nil })

	var buf bytes.Buffer
	if err := r.FPrintHelp(context.Background(), &buf, nil); err != nil {
		t.Fatalf("FPrintHelp returned error: %v", err)
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
	if res.Request == nil {
		t.Fatal("command resolution missing Request")
	}
	if res.Handler == nil {
		t.Fatal("command resolution missing Handler")
	}
	if !res.Executable {
		t.Fatal("Executable = false, want true")
	}
	if !res.Exact {
		t.Fatal("Exact = false, want true")
	}
	if got := res.Request.Params["component"]; got != "api" {
		t.Fatalf("component param = %q, want %q", got, "api")
	}
}

func TestRouter_FPrintHelp_PrintsProvidedScope(t *testing.T) {
	r := New()

	r.Describe("codex model", "Model commands")
	r.Handle("codex model list", "List models", func(req *Request) error { return nil })

	var buf bytes.Buffer
	if err := r.FPrintHelp(context.Background(), &buf, []string{"codex", "model"}); err != nil {
		t.Fatalf("FPrintHelp returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Model commands") {
		t.Fatalf("missing contextual help intro: %q", out)
	}
	if !strings.Contains(out, "codex model list") {
		t.Fatalf("missing child command: %q", out)
	}
}

func TestRouter_Resolve_DescribeRouteResult(t *testing.T) {
	r := New()

	r.Describe("component <component>", "Component root")

	res, err := r.Resolve(context.Background(), []string{"component", "api"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if res.Executable {
		t.Fatal("Executable = true, want false")
	}
	if !res.Exact {
		t.Fatal("Exact = false, want true")
	}
	if got := res.Request.Params["component"]; got != "api" {
		t.Fatalf("component param = %q, want %q", got, "api")
	}
}

func TestRouter_Run_DescribeRouteDoesNotExecute(t *testing.T) {
	r := New()

	r.Describe("component <component>", "Component root")

	err := r.Run(context.Background(), []string{"component", "api"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRouter_FPrintHelp_DescribeRouteAppearsInContextualHelp(t *testing.T) {
	r := New()

	r.Describe("", "Root help.")
	r.Describe("component", "Component commands")

	var buf bytes.Buffer
	if err := r.FPrintHelp(context.Background(), &buf, nil); err != nil {
		t.Fatalf("FPrintHelp returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "component") {
		t.Fatalf("missing describe route from help: %q", out)
	}
	if !strings.Contains(out, "Component commands") {
		t.Fatalf("missing describe route description from help: %q", out)
	}
}

func TestRouter_FPrintHelp_DescribeRouteContributesIntro(t *testing.T) {
	r := New()

	r.Describe("component <component>", "Manage one component.")
	r.Handle("component <component> status", "Show status", func(req *Request) error { return nil })

	var buf bytes.Buffer
	if err := r.FPrintHelp(context.Background(), &buf, []string{"component", "api"}); err != nil {
		t.Fatalf("FPrintHelp returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Manage one component.") {
		t.Fatalf("missing describe intro: %q", out)
	}
	if !strings.Contains(out, "component <component> status") {
		t.Fatalf("missing child command: %q", out)
	}
}

func TestRouter_FPrintHelp_PrefersLiteralScopeOverParameterizedSibling(t *testing.T) {
	r := New()
	noop := func(req *Request) error { return nil }

	r.Handle("thread <thread> status", "Show thread status", noop)
	r.Handle("thread <thread> purge", "Purge thread", noop)
	r.Handle("thread current status", "Show current thread status", noop)
	r.Handle("thread current refresh", "Refresh current thread", noop)

	var buf bytes.Buffer
	if err := r.FPrintHelp(context.Background(), &buf, []string{"thread", "current"}); err != nil {
		t.Fatalf("FPrintHelp returned error: %v", err)
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
