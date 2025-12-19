package clir

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// --- Helpers for tests ---

type appCtx struct {
	Name string
}

type componentCtx struct {
	App  appCtx
	Name string
}

// --- Basic routing tests ---

func TestRouter_HandleAndRun_LiteralMatch(t *testing.T) {
	r := New()

	var called bool
	var gotArgs []string

	r.Handle("version", "Show version", func(req *Request) error {
		called = true
		gotArgs = append([]string{}, req.Args...)
		return nil
	})

	if err := r.Run(context.Background(), []string{"version"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if !called {
		t.Fatal("handler was not called")
	}
	if len(gotArgs) != 1 || gotArgs[0] != "version" {
		t.Fatalf("unexpected args: %#v", gotArgs)
	}
}

func TestRouter_HandleAndRun_ParamAndExtra(t *testing.T) {
	r := New()

	var gotParams Params
	var gotExtra []string

	r.Handle("comp <component> image build", "Build images", func(req *Request) error {
		gotParams = req.Params
		gotExtra = req.Extra
		return nil
	})

	argv := []string{
		"comp", "cv-server", "image", "build", "--tag", "latest", "--push",
	}

	if err := r.Run(context.Background(), argv); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if gotParams == nil || gotParams["component"] != "cv-server" {
		t.Fatalf("unexpected params: %#v", gotParams)
	}

	wantExtra := []string{"--tag", "latest", "--push"}
	if fmt.Sprint(gotExtra) != fmt.Sprint(wantExtra) {
		t.Fatalf("unexpected extra: got %v, want %v", gotExtra, wantExtra)
	}
}

func TestRouter_Run_NoMatch(t *testing.T) {
	r := New()

	r.Handle("foo bar", "Foo bar", func(req *Request) error {
		return nil
	})

	err := r.Run(context.Background(), []string{"no", "match"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no matching command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRouter_Run_FirstMatchWins(t *testing.T) {
	r := New()

	var calls []string

	r.Handle("cmd", "First", func(req *Request) error {
		calls = append(calls, "first")
		return nil
	})
	r.Handle("cmd", "Second", func(req *Request) error {
		calls = append(calls, "second")
		return nil
	})

	if err := r.Run(context.Background(), []string{"cmd"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(calls) != 1 || calls[0] != "first" {
		t.Fatalf("expected only first handler to be called, got %v", calls)
	}
}

// --- Request context tests ---

func TestRequest_Context_DefaultBackgroundWhenNil(t *testing.T) {
	r := New()

	var gotCtx context.Context

	r.Handle("cmd", "Test", func(req *Request) error {
		gotCtx = req.Context()
		return nil
	})

	// Run with nil context; router should use context.Background().
	if err := r.Run(nil, []string{"cmd"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if gotCtx == nil {
		t.Fatal("Request.Context returned nil")
	}
}

// --- PrintHelp tests ---

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

// --- Middleware tests ---

func TestBuilder_WithMiddleware_Order(t *testing.T) {
	r := New()

	var steps []string

	logMiddleware := func(name string) Middleware {
		return func(next Handler) Handler {
			return func(req *Request) error {
				steps = append(steps, "before-"+name)
				err := next(req)
				steps = append(steps, "after-"+name)
				return err
			}
		}
	}

	r.Routes(func(b *Builder) {
		b.With(logMiddleware("outer")).
			With(logMiddleware("inner")).
			Handle("do", "Do something", func(req *Request) error {
				steps = append(steps, "handler")
				return nil
			})
	})

	if err := r.Run(context.Background(), []string{"do"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	want := []string{
		"before-outer",
		"before-inner",
		"handler",
		"after-inner",
		"after-outer",
	}

	if fmt.Sprint(steps) != fmt.Sprint(want) {
		t.Fatalf("unexpected middleware order: got %v, want %v", steps, want)
	}
}

// --- Builder prefix tests ---

func TestBuilder_RoutePrefixesAndHandle(t *testing.T) {
	r := New()

	var matchedPattern string
	r.Routes(func(b *Builder) {
		b.Route("comp <component>", func(b *Builder) {
			b.Route("image", func(b *Builder) {
				b.Handle("build", "Build images", func(req *Request) error {
					matchedPattern = fmt.Sprintf(
						"component=%s extra=%v",
						req.Params["component"], req.Extra,
					)
					return nil
				})
			})
		})
	})

	argv := []string{"comp", "cv-server", "image", "build", "--foo"}
	if err := r.Run(context.Background(), argv); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if matchedPattern != "component=cv-server extra=[--foo]" {
		t.Fatalf("unexpected match: %q", matchedPattern)
	}
}

// --- Typed context tests ---

func TestTypedContext_WithContextHandler_DirectRouterHandle(t *testing.T) {
	r := New()

	resolveComp := func(req *Request) (*componentCtx, error) {
		name, ok := req.Params["component"]
		if !ok {
			return nil, errors.New("missing component param")
		}
		return &componentCtx{
			App:  appCtx{Name: "myapp"},
			Name: name,
		}, nil
	}

	var got componentCtx

	r.Handle("comp <component> info", "Component info",
		WithContextHandler(resolveComp, func(req *Request, c *componentCtx) error {
			got = *c
			return nil
		}),
	)

	if err := r.Run(context.Background(), []string{"comp", "cv-server", "info"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if got.Name != "cv-server" || got.App.Name != "myapp" {
		t.Fatalf("unexpected context: %#v", got)
	}
}

func TestTypedContext_WithContextHandler_ResolverError(t *testing.T) {
	r := New()

	resolveErr := func(req *Request) (componentCtx, error) {
		return componentCtx{}, errors.New("boom")
	}

	r.Handle("comp <component> info", "Component info",
		WithContextHandler(resolveErr, func(req *Request, c componentCtx) error {
			t.Fatal("handler should not be called on resolver error")
			return nil
		}),
	)

	err := r.Run(context.Background(), []string{"comp", "cv-server", "info"})
	if err == nil {
		t.Fatal("expected error from resolver, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTypedContext_ContextBuilder_SingleLayer(t *testing.T) {
	r := New()

	resolveApp := func(req *Request) (appCtx, error) {
		return appCtx{Name: "cli-app"}, nil
	}

	var gotApp appCtx
	var gotParams Params

	r.Routes(func(b *Builder) {
		appB := WithContext(b, resolveApp)

		appB.Handle("ping", "App ping", func(req *Request, ctx appCtx) error {
			gotApp = ctx
			gotParams = req.Params
			return nil
		})
	})

	if err := r.Run(context.Background(), []string{"ping"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if gotApp.Name != "cli-app" {
		t.Fatalf("unexpected app context: %#v", gotApp)
	}
	if gotParams == nil || len(gotParams) != 0 {
		t.Fatalf("unexpected params: %#v", gotParams)
	}
}

func TestTypedContext_LayeredContexts_WithChildContext(t *testing.T) {
	r := New()

	resolveApp := func(req *Request) (appCtx, error) {
		return appCtx{Name: "cli-app"}, nil
	}

	resolveComponent := func(app appCtx, req *Request) (componentCtx, error) {
		name, ok := req.Params["component"]
		if !ok {
			return componentCtx{}, errors.New("missing component")
		}
		return componentCtx{
			App:  app,
			Name: name,
		}, nil
	}

	var gotApp appCtx
	var gotComp componentCtx
	var gotExtra []string

	r.Routes(func(b *Builder) {
		appB := WithContext(b, resolveApp)

		appB.Route("comp <component>", func(b *ContextBuilder[appCtx]) {
			compB := WithChildContext(b, resolveComponent)

			compB.Route("image", func(b *ContextBuilder[componentCtx]) {
				b.Handle("build", "Build images", func(req *Request, ctx componentCtx) error {
					gotApp = ctx.App
					gotComp = ctx
					gotExtra = req.Extra
					return nil
				})
			})
		})
	})

	argv := []string{"comp", "cv-server", "image", "build", "--flag"}
	if err := r.Run(context.Background(), argv); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if gotApp.Name != "cli-app" {
		t.Fatalf("unexpected app ctx: %#v", gotApp)
	}
	if gotComp.Name != "cv-server" {
		t.Fatalf("unexpected component ctx: %#v", gotComp)
	}
	if fmt.Sprint(gotExtra) != "[--flag]" {
		t.Fatalf("unexpected extra: %v", gotExtra)
	}
}

// --- Example-style tests (documentation via go test / go doc) ---

func ExampleRouter_basic() {
	r := New()

	r.Routes(func(b *Builder) {
		b.Handle("hello", "Say hello", func(req *Request) error {
			fmt.Println("hello world")
			return nil
		})
	})

	_ = r.Run(context.Background(), []string{"hello"})
	// Output:
	// hello world
}

func ExampleContextBuilder_layered() {
	r := New()

	// Root resolver for the app context.
	resolveApp := func(req *Request) (appCtx, error) {
		return appCtx{Name: "cli-app"}, nil
	}

	// Child resolver for component context.
	resolveComponent := func(app appCtx, req *Request) (componentCtx, error) {
		return componentCtx{
			App:  app,
			Name: req.Params["component"],
		}, nil
	}

	r.Routes(func(b *Builder) {
		// Lift to a typed app context builder.
		appB := WithContext(b, resolveApp)

		appB.Route("comp <component>", func(b *ContextBuilder[appCtx]) {
			// Derive component context from app context.
			compB := WithChildContext(b, resolveComponent)

			compB.Route("image", func(b *ContextBuilder[componentCtx]) {
				b.Handle("build", "Build images", func(req *Request, ctx componentCtx) error {
					fmt.Printf("app=%s component=%s\n", ctx.App.Name, ctx.Name)
					return nil
				})
			})
		})
	})

	_ = r.Run(context.Background(), []string{"comp", "cv-server", "image", "build"})
	// Output:
	// app=cli-app component=cv-server
}

func TestRouter_bestMatch_RankingMatrix(t *testing.T) {
	type tc struct {
		name       string
		routes     []string
		argv       []string
		wantRoute  string
		wantParams Params
		wantExtra  []string
		wantOK     bool
	}

	tests := []tc{
		{
			name: "static beats param at same depth",
			routes: []string{
				"users <id>",
				"users me",
			},
			argv:       []string{"users", "me"},
			wantRoute:  "users me",
			wantParams: Params{},
			wantExtra:  nil,
			wantOK:     true,
		},
		{
			name: "param route used when no static alternative",
			routes: []string{
				"users <id>",
			},
			argv:      []string{"users", "42"},
			wantRoute: "users <id>",
			wantParams: Params{
				"id": "42",
			},
			wantExtra: nil,
			wantOK:    true,
		},
		{
			name: "longer route wins over shorter prefix",
			routes: []string{
				"comp <component>",
				"comp <component> image build",
			},
			argv:      []string{"comp", "cv-server", "image", "build", "--tag", "latest"},
			wantRoute: "comp <component> image build",
			wantParams: Params{
				"component": "cv-server",
			},
			wantExtra: []string{"--tag", "latest"},
			wantOK:    true,
		},
		{
			name: "static leaf beats param leaf",
			routes: []string{
				"docker image <name> build",
				"docker image list",
			},
			argv:       []string{"docker", "image", "list", "-v"},
			wantRoute:  "docker image list",
			wantParams: Params{},
			wantExtra:  []string{"-v"},
			wantOK:     true,
		},
		{
			name: "deeper static beats shallower param (by rank length)",
			routes: []string{
				"docker image <name>",
				"docker image <name> build",
			},
			argv:      []string{"docker", "image", "alpine", "build"},
			wantRoute: "docker image <name> build",
			wantParams: Params{
				"name": "alpine",
			},
			wantExtra: nil,
			wantOK:    true,
		},
		{
			name: "deeper static beats shallower param (by rank length)",
			routes: []string{
				"a b <c>",
				"a <b> <c> <d>",
			},
			argv:      []string{"a", "b", "x", "y"},
			wantRoute: "a b <c>",
			wantParams: Params{
				"c": "x",
			},
			wantExtra: []string{"y"},
			wantOK:    true,
		},
		{
			name: "no match",
			routes: []string{
				"comp <component> image build",
				"docker image list",
			},
			argv:      []string{"nope"},
			wantRoute: "",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()

			// Map route string -> *route captured at registration time.
			registered := map[string]*route{}

			// Register routes with no-op handlers (we only test matching).
			for _, pat := range tt.routes {
				p := pat
				r.Handle(p, "desc", func(*Request) error { return nil })

				// Capture pointer to the newly-added route.
				// Assumes Handle appends to r.routes (common pattern).
				if len(r.routes) == 0 {
					t.Fatalf("router has no routes after Handle(%q)", p)
				}
				registered[p] = &r.routes[len(r.routes)-1]
			}

			gotRoute, gotReq, ok := r.bestMatch(context.Background(), tt.argv)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				if gotRoute != nil || gotReq != nil {
					t.Fatalf("expected nil route/req on no-match, got route=%v req=%v", gotRoute, gotReq)
				}
				return
			}

			wantPtr := registered[tt.wantRoute]
			if wantPtr == nil {
				t.Fatalf("test setup error: wantRoute %q was not registered", tt.wantRoute)
			}
			if gotRoute.String() != tt.wantRoute {
				t.Fatalf("matched route mismatch: got %q, want %q", gotRoute.String(), tt.wantRoute)
			}

			// Params
			if fmt.Sprint(gotReq.Params) != fmt.Sprint(tt.wantParams) {
				t.Fatalf("params mismatch: got %#v, want %#v", gotReq.Params, tt.wantParams)
			}
			// Extra
			if fmt.Sprint(gotReq.Extra) != fmt.Sprint(tt.wantExtra) {
				t.Fatalf("extra mismatch: got %v, want %v", gotReq.Extra, tt.wantExtra)
			}
		})
	}
}
