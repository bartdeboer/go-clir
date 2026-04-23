package clir

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func routePatterns(routes []*route) []string {
	out := make([]string, len(routes))
	for i, rt := range routes {
		out[i] = rt.String()
	}
	return out
}

func TestRouter_PrefixMatchRoutesAtDepth(t *testing.T) {
	r := New()

	r.Handle("comp <component>", "Component root", func(req *Request) error { return nil })
	r.Handle("comp <component> image", "Image commands", func(req *Request) error { return nil })
	r.Handle("comp <component> image build", "Build images", func(req *Request) error { return nil })
	r.Handle("comp <component> logs", "Logs", func(req *Request) error { return nil })
	r.Handle("status", "Status", func(req *Request) error { return nil })

	t.Run("exact scope depth", func(t *testing.T) {
		matches := r.prefixMatchRoutesAtDepth([]string{"comp", "api"}, 0)
		if len(matches) != 1 {
			t.Fatalf("got %d matches, want 1", len(matches))
		}
		if got := matches[0].String(); got != "comp <component>" {
			t.Fatalf("got %q, want %q", got, "comp <component>")
		}
	})

	t.Run("one extra segment", func(t *testing.T) {
		matches := r.prefixMatchRoutesAtDepth([]string{"comp", "api"}, 1)

		got := strings.Join(routePatterns(matches), "|")
		want := strings.Join([]string{
			"comp <component> image",
			"comp <component> logs",
		}, "|")

		if got != want {
			t.Fatalf("got %v, want %v", routePatterns(matches), strings.Split(want, "|"))
		}
	})

	t.Run("two extra segments", func(t *testing.T) {
		matches := r.prefixMatchRoutesAtDepth([]string{"comp", "api"}, 2)
		if len(matches) != 1 {
			t.Fatalf("got %d matches, want 1", len(matches))
		}
		if got := matches[0].String(); got != "comp <component> image build" {
			t.Fatalf("got %q, want %q", got, "comp <component> image build")
		}
	})

	t.Run("no matches", func(t *testing.T) {
		matches := r.prefixMatchRoutesAtDepth([]string{"deploy"}, 0)
		if len(matches) != 0 {
			t.Fatalf("got %d matches, want 0", len(matches))
		}
	})
}

func TestRouter_FilterDepthRoutes(t *testing.T) {
	r := New()

	r.Handle("comp <component>", "Component root", func(req *Request) error { return nil })
	r.Handle("comp <component> image", "Image commands", func(req *Request) error { return nil })
	r.Handle("comp <component> image build", "Build images", func(req *Request) error { return nil })
	r.Handle("comp <component> logs", "Logs", func(req *Request) error { return nil })
	r.Handle("status", "Status", func(req *Request) error { return nil })

	t.Run("depth zero returns exact scope", func(t *testing.T) {
		matches := r.filterDepthRoutes([]string{"comp", "api"}, 0)

		got := strings.Join(routePatterns(matches), "|")
		want := "comp <component>"

		if got != want {
			t.Fatalf("got %v, want %v", routePatterns(matches), []string{want})
		}
	})

	t.Run("depth one returns scope and children", func(t *testing.T) {
		matches := r.filterDepthRoutes([]string{"comp", "api"}, 1)

		got := strings.Join(routePatterns(matches), "|")
		want := strings.Join([]string{
			"comp <component>",
			"comp <component> image",
			"comp <component> logs",
		}, "|")

		if got != want {
			t.Fatalf("got %v, want %v", routePatterns(matches), strings.Split(want, "|"))
		}
	})

	t.Run("depth two returns scope children and grandchildren", func(t *testing.T) {
		matches := r.filterDepthRoutes([]string{"comp", "api"}, 2)

		got := strings.Join(routePatterns(matches), "|")
		want := strings.Join([]string{
			"comp <component>",
			"comp <component> image",
			"comp <component> image build",
			"comp <component> logs",
		}, "|")

		if got != want {
			t.Fatalf("got %v, want %v", routePatterns(matches), strings.Split(want, "|"))
		}
	})
}

func TestRouter_RunWithHelp_RunsCommandWhenNotHelp(t *testing.T) {
	r := New()

	var called bool
	r.Handle("status", "Show status", func(req *Request) error {
		called = true
		return nil
	})

	if err := r.RunWithHelp(context.Background(), []string{"status"}); err != nil {
		t.Fatalf("RunWithHelp returned error: %v", err)
	}

	if !called {
		t.Fatal("expected command handler to be called")
	}
}

func TestRouter_RunWithHelpTo_PrintsContextualHelp(t *testing.T) {
	r := New()

	r.Handle("comp <component> help", "Manage component commands.", func(req *Request) error { return nil })
	r.Handle("comp <component> image", "Image commands", func(req *Request) error { return nil })
	r.Handle("comp <component> logs", "View logs", func(req *Request) error { return nil })

	var buf bytes.Buffer
	if err := r.RunWithHelpTo(context.Background(), []string{"comp", "api", "help"}, &buf); err != nil {
		t.Fatalf("RunWithHelpTo returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Manage component commands.") {
		t.Fatalf("missing help intro: %q", out)
	}
	if !strings.Contains(out, "Available commands:") {
		t.Fatalf("missing command list header: %q", out)
	}
	if !strings.Contains(out, "comp <component> image") {
		t.Fatalf("missing image command: %q", out)
	}
	if !strings.Contains(out, "comp <component> logs") {
		t.Fatalf("missing logs command: %q", out)
	}
	if strings.Contains(out, "comp <component> help") {
		t.Fatalf("help route should not be listed as a child command: %q", out)
	}
}

func TestRouter_RunWithHelpTo_NoHelpAvailable(t *testing.T) {
	r := New()

	var buf bytes.Buffer
	err := r.RunWithHelpTo(context.Background(), []string{"comp", "api", "help"}, &buf)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no help available") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRouter_PrintHelp_NoCommands(t *testing.T) {
	r := New()
	var buf bytes.Buffer

	r.PrintHelp(&buf)
	out := buf.String()

	if !strings.Contains(out, "No commands registered.") {
		t.Fatalf("unexpected help output: %q", out)
	}
}

func TestRouter_PrintHelp_WithCommandsSorted(t *testing.T) {
	r := New()

	r.Handle("beta", "Beta command", func(req *Request) error { return nil })
	r.Handle("alpha", "Alpha command", func(req *Request) error { return nil })
	r.Handle("gamma", "Gamma command", func(req *Request) error { return nil })

	var buf bytes.Buffer
	r.PrintHelp(&buf)
	out := buf.String()

	if !strings.Contains(out, "Available commands:") {
		t.Fatalf("help output missing header: %q", out)
	}

	alphaIdx := strings.Index(out, "alpha")
	betaIdx := strings.Index(out, "beta")
	gammaIdx := strings.Index(out, "gamma")

	if alphaIdx == -1 || betaIdx == -1 || gammaIdx == -1 {
		t.Fatalf("help output missing commands: %q", out)
	}

	if !(alphaIdx < betaIdx && betaIdx < gammaIdx) {
		t.Fatalf("commands not sorted: alpha=%d beta=%d gamma=%d\n%q",
			alphaIdx, betaIdx, gammaIdx, out)
	}
}
