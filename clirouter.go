// Package clirouter provides a tiny, chi-style router for CLI arguments,
// with http-like Request + context support.
//
// Example:
//
//	r := clirouter.New()
//
//	r.Routes(func(rt *clirouter.RouteBuilder) {
//	    rt.Route("comp <component>", func(rt *clirouter.RouteBuilder) {
//	        rt.Route("image", func(rt *clirouter.RouteBuilder) {
//	            rt.Handle("build", "Build images",
//	                clirouter.WithContext(resolveComponent, func(req *clirouter.Request, c *component.Adapter) error {
//	                    return c.BuildImages(req.Context())
//	                }),
//	            )
//	        })
//	    })
//	})
//
//	if err := r.Run(context.Background(), os.Args[1:]); err != nil {
//	    fmt.Println("Error:", err)
//	    r.PrintHelp(os.Stdout)
//	}
package clirouter

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Params are the named parameters captured from a pattern,
// e.g. "comp <component> image build" + argv "comp cv-server image build"
// => Params{"component": "cv-server"}.
type Params map[string]string

// Request represents a single CLI invocation, similar to http.Request.
type Request struct {
	// ctx is the underlying context for cancellation, deadlines, values.
	ctx context.Context

	// Args is the full argv slice passed to Router.Run (e.g. os.Args[1:]).
	Args []string

	// Params are the named parameters captured from the matched pattern.
	Params Params

	// Extra are the arguments beyond the pattern, e.g. "cli comp x run task y arg1 arg2"
	// when pattern is "comp <component> run task <task>" → Extra{"arg1","arg2"}.
	Extra []string
}

// Context returns the underlying context.
func (r *Request) Context() context.Context {
	if r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}

// WithContext returns a shallow copy of Request with ctx replaced.
func (r *Request) WithContext(ctx context.Context) *Request {
	cp := *r
	cp.ctx = ctx
	return &cp
}

// HandlerFunc receives the composed Request.
type HandlerFunc func(req *Request) error

// Middleware wraps a HandlerFunc, typically to add logging, auth, etc.
type Middleware func(HandlerFunc) HandlerFunc

type segment struct {
	lit   string // non-empty for static segment: "comp", "image", "build"
	param string // non-empty for param segment: e.g. "component" for "<component>"
}

type route struct {
	segments []segment
	handler  HandlerFunc
	desc     string
}

// Router holds all registered routes and can execute them for argv.
type Router struct {
	routes []route
}

// New creates an empty Router.
func New() *Router {
	return &Router{}
}

// Handle registers a pattern, description and handler directly.
//
// Pattern is a space-separated sequence of segments, where
//   - literal words match literally: "comp", "image", "build"
//   - parameters are written as <name>: "<component>", "<task>"
//
// Example:
//
//	r.Handle("comp <component> image build", "Build images", handler)
func (r *Router) Handle(pattern, desc string, h HandlerFunc) {
	parts := strings.Fields(pattern)
	segs := make([]segment, len(parts))

	for i, p := range parts {
		if strings.HasPrefix(p, "<") && strings.HasSuffix(p, ">") {
			segs[i] = segment{param: p[1 : len(p)-1]}
		} else {
			segs[i] = segment{lit: p}
		}
	}

	r.routes = append(r.routes, route{
		segments: segs,
		handler:  h,
		desc:     desc,
	})
}

// Run attempts to match argv against registered routes and executes
// the first matching handler. ctx becomes the root context for the Request.
func (r *Router) Run(ctx context.Context, argv []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, rt := range r.routes {
		params, extra, ok := match(rt.segments, argv)
		if !ok {
			continue
		}
		req := &Request{
			ctx:    ctx,
			Args:   argv,
			Params: params,
			Extra:  extra,
		}
		return rt.handler(req)
	}
	return fmt.Errorf("no matching command")
}

// PrintHelp prints all registered patterns and their descriptions,
// sorted alphabetically by pattern.
func (r *Router) PrintHelp(w io.Writer) {
	if len(r.routes) == 0 {
		fmt.Fprintln(w, "No commands registered.")
		return
	}

	entries := make([]struct {
		pat  string
		desc string
	}, len(r.routes))

	for i, rt := range r.routes {
		var parts []string
		for _, s := range rt.segments {
			if s.lit != "" {
				parts = append(parts, s.lit)
			} else {
				parts = append(parts, "<"+s.param+">")
			}
		}
		entries[i].pat = strings.Join(parts, " ")
		entries[i].desc = rt.desc
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].pat < entries[j].pat
	})

	maxLen := 0
	for _, e := range entries {
		if l := len(e.pat); l > maxLen {
			maxLen = l
		}
	}
	fmt.Fprintln(w, "Available commands:")
	format := fmt.Sprintf("  %%-%ds  %%s\n", maxLen)
	for _, e := range entries {
		fmt.Fprintf(w, format, e.pat, e.desc)
	}
}

// match tries to match argv against the given pattern segments.
func match(segs []segment, argv []string) (Params, []string, bool) {
	if len(argv) < len(segs) {
		return nil, nil, false
	}

	params := Params{}
	for i, s := range segs {
		arg := argv[i]
		switch {
		case s.lit != "":
			if arg != s.lit {
				return nil, nil, false
			}
		case s.param != "":
			params[s.param] = arg
		}
	}

	extra := argv[len(segs):]
	return params, extra, true
}

// Routes is a convenience entry-point to build routes with a RouteBuilder.
func (r *Router) Routes(fn func(rt *RouteBuilder)) {
	fn(&RouteBuilder{
		router: r,
		prefix: nil,
		mws:    nil,
	})
}

// RouteBuilder provides a chi-style API to build routes with prefixes
// and middleware.
type RouteBuilder struct {
	router *Router
	prefix []string
	mws    []Middleware
}

// Route adds a path prefix (space-separated segments) for all routes
// defined in the callback.
//
// Example:
//
//	rt.Route("comp <component>", func(rt *RouteBuilder) {
//	    rt.Route("image", func(rt *RouteBuilder) {
//	        rt.Handle("build", "Build images", handler)
//	    })
//	})
func (rb *RouteBuilder) Route(path string, fn func(r *RouteBuilder)) {
	parts := strings.Fields(path)
	child := &RouteBuilder{
		router: rb.router,
		prefix: append(append([]string{}, rb.prefix...), parts...),
		mws:    append([]Middleware{}, rb.mws...), // copy for isolation
	}
	fn(child)
}

// With adds middleware to all routes defined in the returned builder.
//
// Example:
//
//	rt.With(loggingMiddleware).Route("comp <component>", func(rt *RouteBuilder) {
//	    rt.Handle("list", "List components", handler)
//	})
func (rb *RouteBuilder) With(mws ...Middleware) *RouteBuilder {
	child := &RouteBuilder{
		router: rb.router,
		prefix: append([]string{}, rb.prefix...),
		mws:    append(append([]Middleware{}, rb.mws...), mws...),
	}
	return child
}

// Handle registers a handler under the current prefix + relative path.
//
// Example (under a prefix "comp <component>"):
//
//	rt.Handle("image build", "Build images", handler)
//	// pattern: "comp <component> image build"
func (rb *RouteBuilder) Handle(path, desc string, h HandlerFunc) {
	parts := strings.Fields(path)
	full := append(append([]string{}, rb.prefix...), parts...)
	pattern := strings.Join(full, " ")

	// Apply middleware chain (outermost first).
	wrapped := h
	for i := len(rb.mws) - 1; i >= 0; i-- {
		wrapped = rb.mws[i](wrapped)
	}

	rb.router.Handle(pattern, desc, wrapped)
}

// CtxHandler is a handler that operates on a typed context object
// plus the Request.
type CtxHandler[T any] func(req *Request, ctx T) error

// WithContext lifts a typed handler into a normal HandlerFunc by resolving
// a context object from the Request (typically via Params).
//
// Example:
//
//	resolveComp := func(req *clirouter.Request) (*component.Adapter, error) { ... }
//
//	rt.Handle("image build", "Build images",
//	    clirouter.WithContext(resolveComp, func(req *clirouter.Request, c *component.Adapter) error {
//	        return c.BuildImages(req.Context())
//	    }),
//	)
func WithContext[T any](
	resolve func(*Request) (T, error),
	h CtxHandler[T],
) HandlerFunc {
	return func(req *Request) error {
		ctxObj, err := resolve(req)
		if err != nil {
			return err
		}
		return h(req, ctxObj)
	}
}
