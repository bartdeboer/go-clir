package clirouter_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	. "github.com/bartdeboer/go-clirouter"
)

// helper to run a router and capture any error msg
func runOK(t *testing.T, r *Router, argv ...string) {
	t.Helper()
	if err := r.Run(context.Background(), argv); err != nil {
		t.Fatalf("Run(%v) returned error: %v", argv, err)
	}
}

func runErr(t *testing.T, r *Router, argv ...string) error {
	t.Helper()
	err := r.Run(context.Background(), argv)
	if err == nil {
		t.Fatalf("Run(%v) expected error, got nil", argv)
	}
	return err
}

// --- basic matching and params ---

func TestRouterLiteralAndParams(t *testing.T) {
	r := New()

	var gotPattern string
	var gotParams Params
	var gotExtra []string

	r.Handle("comp <component> image build", "Build images", func(req *Request) error {
		gotPattern = "comp <component> image build"
		gotParams = req.Params
		gotExtra = req.Extra
		return nil
	})

	runOK(t, r, "comp", "cv-server", "image", "build", "arg1", "arg2")

	if gotPattern == "" {
		t.Fatalf("handler was not called")
	}
	if gotParams["component"] != "cv-server" {
		t.Fatalf("expected component=cv-server, got %q", gotParams["component"])
	}
	if len(gotExtra) != 2 || gotExtra[0] != "arg1" || gotExtra[1] != "arg2" {
		t.Fatalf("expected extra [arg1 arg2], got %v", gotExtra)
	}
}

func TestRouterNoMatch(t *testing.T) {
	r := New()
	r.Handle("comp <component> image build", "Build images", func(req *Request) error { return nil })

	err := r.Run(context.Background(), []string{"comp", "cv-server", "deploy", "create"})
	if err == nil {
		t.Fatalf("expected error for non-matching command")
	}
	if !strings.Contains(err.Error(), "no matching command") {
		t.Fatalf("expected 'no matching command' error, got %v", err)
	}
}

// --- RouteBuilder nesting ---

func TestRouteBuilderNestedPatterns(t *testing.T) {
	r := New()

	var called []string

	r.Routes(func(rt *RouteBuilder) {
		rt.Route("comp <component>", func(rt *RouteBuilder) {
			rt.Route("image", func(rt *RouteBuilder) {
				rt.Handle("build", "Build images", func(req *Request) error {
					called = append(called, "image-build:"+req.Params["component"])
					return nil
				})
			})

			rt.Route("deploy", func(rt *RouteBuilder) {
				rt.Handle("create", "Create deploy", func(req *Request) error {
					called = append(called, "deploy-create:"+req.Params["component"])
					return nil
				})
			})
		})
	})

	runOK(t, r, "comp", "cv-server", "image", "build")
	runOK(t, r, "comp", "api", "deploy", "create")

	if len(called) != 2 {
		t.Fatalf("expected 2 handler calls, got %d (%v)", len(called), called)
	}
	if called[0] != "image-build:cv-server" {
		t.Fatalf("unexpected first call: %q", called[0])
	}
	if called[1] != "deploy-create:api" {
		t.Fatalf("unexpected second call: %q", called[1])
	}
}

// --- Middleware and With() ---

func TestMiddlewareOrder(t *testing.T) {
	r := New()

	var trace []string

	logMW := func(name string) Middleware {
		return func(next HandlerFunc) HandlerFunc {
			return func(req *Request) error {
				trace = append(trace, "before:"+name)
				err := next(req)
				trace = append(trace, "after:"+name)
				return err
			}
		}
	}

	r.Routes(func(rt *RouteBuilder) {
		rt.With(logMW("outer")).Route("comp <component>", func(rt *RouteBuilder) {
			rt.With(logMW("inner")).Handle("image build", "Build images", func(req *Request) error {
				trace = append(trace, "handler:"+req.Params["component"])
				return nil
			})
		})
	})

	runOK(t, r, "comp", "cv-server", "image", "build")

	want := []string{
		"before:outer",
		"before:inner",
		"handler:cv-server",
		"after:inner",
		"after:outer",
	}
	if len(trace) != len(want) {
		t.Fatalf("expected trace len=%d, got %d (%v)", len(want), len(trace), trace)
	}
	for i := range want {
		if trace[i] != want[i] {
			t.Fatalf("trace[%d]: got %q, want %q (full trace: %v)", i, trace[i], want[i], trace)
		}
	}
}

// --- WithContext() resolution ---

type fakeComponent struct {
	name string
}

func TestWithContextResolution(t *testing.T) {
	r := New()

	resolveFake := func(req *Request) (*fakeComponent, error) {
		name, ok := req.Params["component"]
		if !ok || name == "" {
			return nil, errors.New("missing component param")
		}
		return &fakeComponent{name: name}, nil
	}

	var seen string

	r.Routes(func(rt *RouteBuilder) {
		rt.Route("comp <component>", func(rt *RouteBuilder) {
			rt.Route("image", func(rt *RouteBuilder) {
				rt.Handle("build", "Build images",
					WithContext(resolveFake, func(req *Request, c *fakeComponent) error {
						seen = c.name
						// ensure context is non-nil
						if req.Context() == nil {
							t.Fatalf("req.Context() is nil")
						}
						return nil
					}),
				)
			})
		})
	})

	runOK(t, r, "comp", "cv-server", "image", "build")

	if seen != "cv-server" {
		t.Fatalf("expected resolved name 'cv-server', got %q", seen)
	}
}

// --- PrintHelp ---

func TestPrintHelpContainsPatterns(t *testing.T) {
	r := New()

	r.Handle("comp <component> image build", "Build images", func(req *Request) error { return nil })
	r.Handle("comp <component> deploy create", "Create deployment", func(req *Request) error { return nil })

	var buf bytes.Buffer
	r.PrintHelp(&buf)
	out := buf.String()

	if !strings.Contains(out, "comp <component> image build") {
		t.Fatalf("help output missing image build pattern: %q", out)
	}
	if !strings.Contains(out, "Build images") {
		t.Fatalf("help output missing image build description: %q", out)
	}
	if !strings.Contains(out, "comp <component> deploy create") {
		t.Fatalf("help output missing deploy create pattern: %q", out)
	}
	if !strings.Contains(out, "Create deployment") {
		t.Fatalf("help output missing deploy create description: %q", out)
	}
}

// --- Request.Context override ---

func TestRequestWithContextOverride(t *testing.T) {
	r := New()

	var ctxValBefore, ctxValAfter string

	type ctxKey struct{}

	r.Handle("hello", "test", func(req *Request) error {
		// base context
		if v, ok := req.Context().Value(ctxKey{}).(string); ok {
			ctxValBefore = v
		}

		// override
		req2 := req.WithContext(context.WithValue(req.Context(), ctxKey{}, "world"))
		if v, ok := req2.Context().Value(ctxKey{}).(string); ok {
			ctxValAfter = v
		}
		return nil
	})

	ctx := context.WithValue(context.Background(), ctxKey{}, "hello")
	if err := r.Run(ctx, []string{"hello"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if ctxValBefore != "hello" {
		t.Fatalf("expected ctxValBefore=hello, got %q", ctxValBefore)
	}
	if ctxValAfter != "world" {
		t.Fatalf("expected ctxValAfter=world, got %q", ctxValAfter)
	}
}

func TestMiddlewareIsolationBetweenBranches(t *testing.T) {
	r := New()

	type ctxKey struct{}

	var trace []string
	var ctxVals []string

	// Middleware that tags the route and injects a context value.
	mw := func(label string) Middleware {
		return func(next HandlerFunc) HandlerFunc {
			return func(req *Request) error {
				trace = append(trace, "mw:"+label)
				req2 := req.WithContext(context.WithValue(req.Context(), ctxKey{}, label))
				return next(req2)
			}
		}
	}

	r.Routes(func(rt *RouteBuilder) {
		// Branch A: has middleware
		rt.With(mw("A")).Route("comp <component>", func(rt *RouteBuilder) {
			rt.Handle("image build", "Build images", func(req *Request) error {
				trace = append(trace, "handler:A:"+req.Params["component"])

				if v, ok := req.Context().Value(ctxKey{}).(string); ok {
					ctxVals = append(ctxVals, "A:"+v)
				} else {
					ctxVals = append(ctxVals, "A:<nil>")
				}
				return nil
			})
		})

		// Branch B: no middleware
		rt.Route("tools", func(rt *RouteBuilder) {
			rt.Handle("list", "List tools", func(req *Request) error {
				trace = append(trace, "handler:B")

				if v, ok := req.Context().Value(ctxKey{}).(string); ok {
					ctxVals = append(ctxVals, "B:"+v)
				} else {
					ctxVals = append(ctxVals, "B:<nil>")
				}
				return nil
			})
		})
	})

	// Call the two independent branches.
	runOK(t, r, "comp", "cv-server", "image", "build")
	runOK(t, r, "tools", "list")

	// We expect middleware only on branch A, not B.
	// Order: mw:A -> handler:A -> handler:B
	wantTrace := []string{
		"mw:A",
		"handler:A:cv-server",
		"handler:B",
	}
	if len(trace) != len(wantTrace) {
		t.Fatalf("trace len = %d, want %d (%v)", len(trace), len(wantTrace), trace)
	}
	for i := range wantTrace {
		if trace[i] != wantTrace[i] {
			t.Fatalf("trace[%d] = %q, want %q (full trace: %v)", i, trace[i], wantTrace[i], trace)
		}
	}

	// Context injected by mw("A") is visible only in branch A.
	if len(ctxVals) != 2 {
		t.Fatalf("ctxVals len = %d, want 2 (%v)", len(ctxVals), ctxVals)
	}
	if ctxVals[0] != "A:A" {
		t.Fatalf("ctxVals[0] = %q, want \"A:A\"", ctxVals[0])
	}
	if ctxVals[1] != "B:<nil>" {
		t.Fatalf("ctxVals[1] = %q, want \"B:<nil>\"", ctxVals[1])
	}
}
