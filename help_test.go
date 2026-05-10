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

func TestRouter_descendantRoutes(t *testing.T) {
	r := New()

	r.Handle("comp <component>", "Component root", func(req *Request) error { return nil })
	r.Handle("comp <component> image", "Image commands", func(req *Request) error { return nil })
	r.Handle("comp <component> image build", "Build images", func(req *Request) error { return nil })
	r.Handle("comp <component> logs", "Logs", func(req *Request) error { return nil })
	r.Handle("status", "Status", func(req *Request) error { return nil })

	t.Run("returns all descendants", func(t *testing.T) {
		matches := r.descendantRoutes([]string{"comp", "api"})

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
		matches := r.descendantRoutes([]string{"deploy"})
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

	r.Describe("comp <component> help", "Manage component commands.")
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

	r.Describe("help", "Root command help.")
	r.Handle("comp", "Manage components", noop)
	r.Describe("comp help", "Manage components in the current deployment.")
	r.Handle("comp list", "List components", noop)
	r.Handle("comp <component>", "Operate on one component", noop)
	r.Describe("comp <component> help", "Manage one component.")
	r.Handle("comp <component> image", "Image commands", noop)
	r.Describe("comp <component> image help", "Build and publish component images.")
	r.Handle("comp <component> image build", "Build image", noop)
	r.Handle("comp <component> image push", "Push image", noop)
	r.Handle("comp <component> logs", "Stream logs", noop)
	r.Handle("comp <component> task", "Task commands", noop)
	r.Describe("comp <component> task help", "Run operational tasks.")
	r.Handle("comp <component> task list", "List tasks", noop)
	r.Handle("comp <component> task run", "Run task", noop)
	r.Handle("env", "Manage environments", noop)
	r.Describe("env help", "Manage deployment environments.")
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

func TestRouter_FRunWithHelp_ConsolidatesImplicitChildPrefixes(t *testing.T) {
	r := New()
	noop := func(req *Request) error { return nil }

	r.Describe("gcloud help", "Google Cloud commands.")
	r.Handle("gcloud login", "Authenticate with gcloud", noop)
	r.Describe("gcloud cluster help", "Cluster commands.")
	r.Handle("gcloud cluster configure", "Configure current cluster context", noop)
	r.Handle("gcloud cluster create <name>", "Create cluster from configuration", noop)
	r.Handle("gcloud image <name> upload", "Upload image to remote registry", noop)
	r.Handle("gcloud dns list", "List DNS auth records", noop)

	var buf bytes.Buffer
	if err := r.FRunWithHelp(context.Background(), &buf, []string{"gcloud", "help"}); err != nil {
		t.Fatalf("FRunWithHelp returned error: %v", err)
	}

	want := "" +
		"Google Cloud commands.\n" +
		"\n" +
		"Available commands:\n" +
		"  gcloud cluster  Cluster commands.\n" +
		"  gcloud dns\n" +
		"  gcloud image\n" +
		"  gcloud login    Authenticate with gcloud\n"

	if got := buf.String(); got != want {
		t.Fatalf("unexpected help output\ngot:\n%q\nwant:\n%q", got, want)
	}
}

func TestRouter_FRunWithHelp_ConsolidationDescriptionPolicy(t *testing.T) {
	r := New()
	noop := func(req *Request) error { return nil }

	r.Describe("tool help", "Tool commands.")
	r.Handle("tool direct", "Direct command", noop)
	r.Describe("tool direct help", "Direct help text should not override the command.")
	r.Describe("tool group help", "Group commands.")
	r.Handle("tool group run", "Run group task", noop)
	r.Handle("tool empty run", "Run empty task", noop)

	tests := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "consolidated help",
			argv: []string{"tool", "help"},
			want: "" +
				"Tool commands.\n" +
				"\n" +
				"Available commands:\n" +
				"  tool direct  Direct command\n" +
				"  tool empty\n" +
				"  tool group   Group commands.\n",
		},
		{
			name: "all descendants",
			argv: []string{"tool", "help", "all"},
			want: "" +
				"Tool commands.\n" +
				"\n" +
				"Available commands:\n" +
				"  tool direct     Direct command\n" +
				"  tool empty run  Run empty task\n" +
				"  tool group run  Run group task\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := r.FRunWithHelp(context.Background(), &buf, tt.argv); err != nil {
				t.Fatalf("FRunWithHelp returned error: %v", err)
			}

			if got := buf.String(); got != tt.want {
				t.Fatalf("unexpected help output\ngot:\n%q\nwant:\n%q", got, tt.want)
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

	r.Describe("help", "Root help.")
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

func TestRouter_HiddenRoute_RunsButIsExcludedFromHelpByDefault(t *testing.T) {
	r := New()

	var called bool
	r.Handle("visible", "Visible command", func(req *Request) error { return nil })
	r.Handle("alias", "Hidden alias", func(req *Request) error {
		called = true
		return nil
	}, Hidden())

	if err := r.Run(context.Background(), []string{"alias"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !called {
		t.Fatal("expected hidden route handler to run")
	}

	var buf bytes.Buffer
	r.PrintHelp(&buf)

	out := buf.String()
	if strings.Contains(out, "alias") {
		t.Fatalf("hidden route should not appear in default help: %q", out)
	}
	if !strings.Contains(out, "visible") {
		t.Fatalf("visible route missing from help: %q", out)
	}
}

func TestRouter_PrintHelp_CanIncludeHiddenRoutes(t *testing.T) {
	r := New()

	r.Handle("visible", "Visible command", func(req *Request) error { return nil })
	r.Handle("alias", "Hidden alias", func(req *Request) error { return nil }, Hidden())

	var buf bytes.Buffer
	r.PrintHelp(&buf, IncludeHidden())

	out := buf.String()
	if !strings.Contains(out, "alias") {
		t.Fatalf("hidden route should appear when IncludeHidden is true: %q", out)
	}
	if !strings.Contains(out, "visible") {
		t.Fatalf("visible route missing from help: %q", out)
	}
}

func TestRouter_FPrintHelp_FiltersContextualHelpByTag(t *testing.T) {
	r := New()
	noop := func(req *Request) error { return nil }

	r.Describe("help", "Root help.")
	r.Handle("status", "Show status", noop, Tag("common"))
	r.Handle("model list", "List models", noop, Tag("common"))
	r.Handle("model set <model>", "Set model", noop, Tag("advanced"))
	r.Handle("refresh", "Legacy refresh alias", noop, Hidden(), Tag("common"))

	commonOnly := FilterHelp(func(info RouteInfo) bool {
		return info.HasTag("common")
	})

	var buf bytes.Buffer
	if err := r.FPrintHelp(context.Background(), &buf, []string{"help", "all"}, commonOnly); err != nil {
		t.Fatalf("FPrintHelp returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "status") {
		t.Fatalf("common route missing from help: %q", out)
	}
	if !strings.Contains(out, "model list") {
		t.Fatalf("common nested route missing from help: %q", out)
	}
	if strings.Contains(out, "model set") {
		t.Fatalf("advanced route should be filtered out: %q", out)
	}
	if strings.Contains(out, "refresh") {
		t.Fatalf("hidden route should remain filtered out by default: %q", out)
	}
}

func TestBuilder_Handle_AcceptsRouteOptions(t *testing.T) {
	r := New()

	r.Routes(func(b *Builder) {
		b.Route("tool", func(b *Builder) {
			b.Handle("visible", "Visible command", func(req *Request) error { return nil }, Tag("common"))
			b.Handle("alias", "Hidden alias", func(req *Request) error { return nil }, Hidden())
		})
	})

	var buf bytes.Buffer
	r.PrintHelp(&buf, FilterHelp(func(info RouteInfo) bool {
		return info.HasTag("common")
	}))

	out := buf.String()
	if !strings.Contains(out, "tool visible") {
		t.Fatalf("tagged builder route missing from help: %q", out)
	}
	if strings.Contains(out, "tool alias") {
		t.Fatalf("hidden builder route should not appear in help: %q", out)
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
