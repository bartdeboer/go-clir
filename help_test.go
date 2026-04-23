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

func TestRouter_DescendantRoutes(t *testing.T) {
	r := New()

	r.Handle("comp <component>", "Component root", func(req *Request) error { return nil })
	r.Handle("comp <component> image", "Image commands", func(req *Request) error { return nil })
	r.Handle("comp <component> image build", "Build images", func(req *Request) error { return nil })
	r.Handle("comp <component> logs", "Logs", func(req *Request) error { return nil })
	r.Handle("status", "Status", func(req *Request) error { return nil })

	t.Run("zero levels returns none", func(t *testing.T) {
		matches := r.descendantRoutes([]string{"comp", "api"}, 0)
		if len(matches) != 0 {
			t.Fatalf("got %d matches, want 0", len(matches))
		}
	})

	t.Run("one level returns direct children", func(t *testing.T) {
		matches := r.descendantRoutes([]string{"comp", "api"}, 1)

		got := strings.Join(routePatterns(matches), "|")
		want := strings.Join([]string{
			"comp <component> image",
			"comp <component> logs",
		}, "|")

		if got != want {
			t.Fatalf("got %v, want %v", routePatterns(matches), strings.Split(want, "|"))
		}
	})

	t.Run("two levels returns children and grandchildren", func(t *testing.T) {
		matches := r.descendantRoutes([]string{"comp", "api"}, 2)

		got := strings.Join(routePatterns(matches), "|")
		want := strings.Join([]string{
			"comp <component> image",
			"comp <component> image build",
			"comp <component> logs",
		}, "|")

		if got != want {
			t.Fatalf("got %v, want %v", routePatterns(matches), strings.Split(want, "|"))
		}
	})

	t.Run("no matches", func(t *testing.T) {
		matches := r.descendantRoutes([]string{"deploy"}, 1)
		if len(matches) != 0 {
			t.Fatalf("got %d matches, want 0", len(matches))
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

func TestRouter_FRunWithHelp_PrintsContextualHelp(t *testing.T) {
	r := New()

	r.Handle("comp <component> help", "Manage component commands.", func(req *Request) error { return nil })
	r.Handle("comp <component> image", "Image commands", func(req *Request) error { return nil })
	r.Handle("comp <component> logs", "View logs", func(req *Request) error { return nil })

	var buf bytes.Buffer
	if err := r.FRunWithHelp(context.Background(), &buf, []string{"comp", "api", "help"}); err != nil {
		t.Fatalf("FRunWithHelp returned error: %v", err)
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

func TestRouter_FRunWithHelp_PrintsExactContextualHelpForCommandTree(t *testing.T) {
	r := New()
	noop := func(req *Request) error { return nil }

	r.Handle("help", "Root command help.", noop)
	r.Handle("comp", "Manage components", noop)
	r.Handle("comp help", "Manage components in the current deployment.", noop)
	r.Handle("comp list", "List components", noop)
	r.Handle("comp <component>", "Operate on one component", noop)
	r.Handle("comp <component> help", "Manage one component.", noop)
	r.Handle("comp <component> image", "Image commands", noop)
	r.Handle("comp <component> image help", "Build and publish component images.", noop)
	r.Handle("comp <component> image build", "Build image", noop)
	r.Handle("comp <component> image push", "Push image", noop)
	r.Handle("comp <component> logs", "Stream logs", noop)
	r.Handle("comp <component> task", "Task commands", noop)
	r.Handle("comp <component> task help", "Run operational tasks.", noop)
	r.Handle("comp <component> task list", "List tasks", noop)
	r.Handle("comp <component> task run", "Run task", noop)
	r.Handle("env", "Manage environments", noop)
	r.Handle("env help", "Manage deployment environments.", noop)
	r.Handle("env list", "List environments", noop)
	r.Handle("env <environment>", "Operate on one environment", noop)
	r.Handle("env <environment> promote", "Promote environment", noop)
	r.Handle("env <environment> rollback", "Rollback environment", noop)
	r.Handle("status", "Show status", noop)

	tests := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "root",
			argv: []string{"help"},
			want: "" +
				"Root command help.\n" +
				"\n" +
				"Available commands:\n" +
				"  comp    Manage components\n" +
				"  env     Manage environments\n" +
				"  status  Show status\n",
		},
		{
			name: "literal prefix",
			argv: []string{"comp", "help"},
			want: "" +
				"Manage components in the current deployment.\n" +
				"\n" +
				"Available commands:\n" +
				"  comp <component>  Operate on one component\n" +
				"  comp list         List components\n",
		},
		{
			name: "parameter prefix",
			argv: []string{"comp", "api", "help"},
			want: "" +
				"Manage one component.\n" +
				"\n" +
				"Available commands:\n" +
				"  comp <component> image  Image commands\n" +
				"  comp <component> logs   Stream logs\n" +
				"  comp <component> task   Task commands\n",
		},
		{
			name: "nested parameter prefix",
			argv: []string{"comp", "api", "image", "help"},
			want: "" +
				"Build and publish component images.\n" +
				"\n" +
				"Available commands:\n" +
				"  comp <component> image build  Build image\n" +
				"  comp <component> image push   Push image\n",
		},
		{
			name: "parameter prefix all descendants",
			argv: []string{"comp", "api", "help", "all"},
			want: "" +
				"Manage one component.\n" +
				"\n" +
				"Available commands:\n" +
				"  comp <component> image        Image commands\n" +
				"  comp <component> image build  Build image\n" +
				"  comp <component> image push   Push image\n" +
				"  comp <component> logs         Stream logs\n" +
				"  comp <component> task         Task commands\n" +
				"  comp <component> task list    List tasks\n" +
				"  comp <component> task run     Run task\n",
		},
		{
			name: "nested prefix",
			argv: []string{"comp", "api", "task", "help"},
			want: "" +
				"Run operational tasks.\n" +
				"\n" +
				"Available commands:\n" +
				"  comp <component> task list  List tasks\n" +
				"  comp <component> task run   Run task\n",
		},
		{
			name: "prefix without explicit help route",
			argv: []string{"env", "prod", "help"},
			want: "" +
				"Available commands:\n" +
				"  env <environment> promote   Promote environment\n" +
				"  env <environment> rollback  Rollback environment\n",
		},
		{
			name: "root all descendants",
			argv: []string{"help", "all"},
			want: "" +
				"Root command help.\n" +
				"\n" +
				"Available commands:\n" +
				"  comp                          Manage components\n" +
				"  comp <component>              Operate on one component\n" +
				"  comp <component> image        Image commands\n" +
				"  comp <component> image build  Build image\n" +
				"  comp <component> image push   Push image\n" +
				"  comp list                     List components\n" +
				"  comp <component> logs         Stream logs\n" +
				"  comp <component> task         Task commands\n" +
				"  comp <component> task list    List tasks\n" +
				"  comp <component> task run     Run task\n" +
				"  env                           Manage environments\n" +
				"  env <environment>             Operate on one environment\n" +
				"  env list                      List environments\n" +
				"  env <environment> promote     Promote environment\n" +
				"  env <environment> rollback    Rollback environment\n" +
				"  status                        Show status\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := r.FRunWithHelp(context.Background(), &buf, tt.argv); err != nil {
				t.Fatalf("FRunWithHelp returned error: %v", err)
			}

			if got := buf.String(); got != tt.want {
				t.Fatalf("unexpected help output for argv %v\ngot:\n%q\nwant:\n%q", tt.argv, got, tt.want)
			}
		})
	}
}

func TestRouter_FRunWithHelp_NoHelpAvailable(t *testing.T) {
	r := New()

	var buf bytes.Buffer
	err := r.FRunWithHelp(context.Background(), &buf, []string{"comp", "api", "help"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no help available") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRouter_FRunWithHelp_InlineFormatter(t *testing.T) {
	r := New()
	r.SetHelpEntryFormatter(WriteHelpInline)

	r.Handle("help", "Root help.", func(req *Request) error { return nil })
	r.Handle("alpha", "Alpha command", func(req *Request) error { return nil })
	r.Handle("beta", "Beta command", func(req *Request) error { return nil })

	var buf bytes.Buffer
	if err := r.FRunWithHelp(context.Background(), &buf, []string{"help"}); err != nil {
		t.Fatalf("FRunWithHelp returned error: %v", err)
	}

	want := "" +
		"Root help.\n" +
		"\n" +
		"Available commands:\n" +
		"alpha - Alpha command\n" +
		"beta - Beta command\n"

	if got := buf.String(); got != want {
		t.Fatalf("unexpected help output\ngot:\n%q\nwant:\n%q", got, want)
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

func TestRouter_PrintHelp_InlineFormatter(t *testing.T) {
	r := New()
	r.SetHelpEntryFormatter(WriteHelpInline)

	r.Handle("beta", "Beta command", func(req *Request) error { return nil })
	r.Handle("alpha", "Alpha command", func(req *Request) error { return nil })

	var buf bytes.Buffer
	r.PrintHelp(&buf)

	want := "" +
		"alpha - Alpha command\n" +
		"beta - Beta command\n"

	if got := buf.String(); got != want {
		t.Fatalf("unexpected help output\ngot:\n%q\nwant:\n%q", got, want)
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
