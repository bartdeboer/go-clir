// Package clir provides a tiny, chi-style router for CLI arguments,
// with http-like Request + context support and optional typed contexts.
//
// Example:
//
//	r := clir.New()
//
//	r.Routes(func(b *clir.Builder) {
//	    // Lift to an app-level typed context:
//	    app := clir.WithContext(b, resolveApp)
//
//	    app.Route("comp <component>", func(b *clir.ContextBuilder[AppCtx]) {
//	        // Derive a component context from the app context:
//	        comp := clir.WithChildContext(b, resolveComponent)
//
//	        comp.Route("image", func(b *clir.ContextBuilder[*component.Adapter]) {
//	            b.Handle("build", "Build images",
//	                func(req *clir.Request, c *component.Adapter) error {
//	                    return c.BuildImages(req.Context())
//	                },
//	            )
//	        })
//	    })
//	})
//
//	if err := r.Run(context.Background(), os.Args[1:]); err != nil {
//	    fmt.Println("Error:", err)
//	    r.PrintHelp(os.Stdout)
//	}
package clir

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
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

	// Extra are the arguments beyond the pattern, e.g.
	// "cli comp x run task y arg1 arg2"
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

// Handler receives the composed Request.
type Handler func(req *Request) error

// Middleware wraps a Handler, typically to add logging, auth, etc.
type Middleware func(Handler) Handler

type segment struct {
	lit   string // non-empty for static segment: "comp", "image", "build"
	param string // non-empty for param segment: e.g. "component" for "<component>"
	sort  int    // optional sort/level hint derived from numeric prefixes
}

type route struct {
	segments []segment
	handler  Handler
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

// parseSegments converts pattern parts into segments, interpreting
// leading integer tokens as sort/level hints for the next segment.
//
// Example parts:
//
//	["1", "comp", "<component>", "2", "image", "build"]
//
// => segments:
//
//	{lit:"comp", sort:1}, {param:"component", sort:0},
//	{lit:"image", sort:2}, {lit:"build", sort:0}
func parseSegments(parts []string) []segment {
	segs := make([]segment, 0, len(parts))
	var pendingSort int

	for _, p := range parts {
		// If it's a pure integer, treat it as a sort hint for the next segment.
		if n, err := strconv.Atoi(p); err == nil {
			pendingSort = n
			continue
		}

		s := segment{sort: pendingSort}
		pendingSort = 0

		if strings.HasPrefix(p, "<") && strings.HasSuffix(p, ">") {
			s.param = p[1 : len(p)-1]
		} else {
			s.lit = p
		}
		segs = append(segs, s)
	}

	return segs
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
func (r *Router) Handle(pattern, desc string, h Handler) {
	parts := strings.Fields(pattern)
	segs := parseSegments(parts)

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
		pat     string
		sortPat string
		desc    string
	}, len(r.routes))

	for i, rt := range r.routes {
		var parts []string
		var sortParts []string
		for _, s := range rt.segments {
			if s.lit != "" {
				parts = append(parts, s.lit)
				sortParts = append(sortParts, fmt.Sprintf("%d %s", s.sort, s.lit))
			} else {
				parts = append(parts, "<"+s.param+">")
			}
		}
		entries[i].pat = strings.Join(parts, " ")
		entries[i].sortPat = strings.Join(sortParts, " ")
		entries[i].desc = rt.desc
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].sortPat < entries[j].sortPat
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

// Routes is a convenience entry-point to build routes with a Builder.
func (r *Router) Routes(fn func(b *Builder)) {
	fn(&Builder{
		router: r,
		prefix: nil,
		mws:    nil,
	})
}

// ---- Builder ----

// Builder provides a chi-style API to build routes with prefixes
// and middleware (untyped).
type Builder struct {
	router *Router
	prefix []string
	mws    []Middleware
}

// Route adds a path prefix (space-separated segments) for all routes
// defined in the callback.
//
// Example:
//
//	b.Route("comp <component>", func(b *Builder) {
//	    b.Route("image", func(b *Builder) {
//	        b.Handle("build", "Build images", handler)
//	    })
//	})
func (b *Builder) Route(path string, fn func(b *Builder)) {
	parts := strings.Fields(path)
	child := &Builder{
		router: b.router,
		prefix: append(append([]string{}, b.prefix...), parts...),
		mws:    append([]Middleware{}, b.mws...), // copy for isolation
	}
	fn(child)
}

// With adds middleware to all routes defined in the returned builder.
//
// Example:
//
//	b.With(loggingMiddleware).Route("comp <component>", func(b *Builder) {
//	    b.Handle("list", "List components", handler)
//	})
func (b *Builder) With(mws ...Middleware) *Builder {
	return &Builder{
		router: b.router,
		prefix: append([]string{}, b.prefix...),
		mws:    append(append([]Middleware{}, b.mws...), mws...),
	}
}

// Handle registers a handler under the current prefix + relative path.
//
// Example (under a prefix "comp <component>"):
//
//	b.Handle("image build", "Build images", handler)
//	// pattern: "comp <component> image build"
func (b *Builder) Handle(path, desc string, h Handler) {
	parts := strings.Fields(path)
	full := append(append([]string{}, b.prefix...), parts...)
	pattern := strings.Join(full, " ")

	// Apply middleware chain (outermost first).
	wrapped := h
	for i := len(b.mws) - 1; i >= 0; i-- {
		wrapped = b.mws[i](wrapped)
	}

	b.router.Handle(pattern, desc, wrapped)
}

// ---- Typed context support ----

// Resolver resolves a typed context object T from the Request.
type Resolver[T any] func(*Request) (T, error)

// ContextHandler is a handler that operates on a typed context object
// plus the Request.
type ContextHandler[T any] func(req *Request, ctx T) error

// ContextBuilder is a typed variant of Builder.
// It shares the same prefix/router/middleware machinery but
// resolves a typed context T for each handler.
type ContextBuilder[T any] struct {
	base    *Builder
	resolve Resolver[T]
}

// Route adds a path prefix (space-separated segments) for all routes
// defined in the callback, keeping the same typed context T.
func (b *ContextBuilder[T]) Route(path string, fn func(b *ContextBuilder[T])) {
	childBase := &Builder{
		router: b.base.router,
		prefix: append(append([]string{}, b.base.prefix...), strings.Fields(path)...),
		mws:    append([]Middleware{}, b.base.mws...), // copy
	}
	fn(&ContextBuilder[T]{
		base:    childBase,
		resolve: b.resolve,
	})
}

// With adds middleware to all routes defined in the returned typed builder.
func (b *ContextBuilder[T]) With(mws ...Middleware) *ContextBuilder[T] {
	childBase := &Builder{
		router: b.base.router,
		prefix: append([]string{}, b.base.prefix...),
		mws:    append(append([]Middleware{}, b.base.mws...), mws...),
	}
	return &ContextBuilder[T]{
		base:    childBase,
		resolve: b.resolve,
	}
}

// Handle registers a typed handler under the current prefix + path.
//
// The handler receives both the Request and the resolved context T.
func (b *ContextBuilder[T]) Handle(path, desc string, h ContextHandler[T]) {
	parts := strings.Fields(path)
	full := append(append([]string{}, b.base.prefix...), parts...)
	pattern := strings.Join(full, " ")

	baseHandler := func(req *Request) error {
		ctxObj, err := b.resolve(req)
		if err != nil {
			return err
		}
		return h(req, ctxObj)
	}

	wrapped := baseHandler
	for i := len(b.base.mws) - 1; i >= 0; i-- {
		wrapped = b.base.mws[i](wrapped)
	}

	b.base.router.Handle(pattern, desc, wrapped)
}

// WithContext lifts an untyped Builder into a typed
// ContextBuilder[T]. This is a package-level generic
// function because methods can't have type parameters.
//
// Example:
//
//	r.Routes(func(b *clir.Builder) {
//	    app := clir.WithContext(b, resolveApp)
//	    // app is *ContextBuilder[AppCtx]
//	})
func WithContext[T any](b *Builder, resolve Resolver[T]) *ContextBuilder[T] {
	return &ContextBuilder[T]{
		base:    b,
		resolve: resolve,
	}
}

// WithChildContext derives a new typed context U from the parent
// typed context T and the Request, for an existing typed builder.
//
// Example:
//
//	app := clir.WithContext(b, resolveApp)
//	app.Route("comp <component>", func(b *clir.ContextBuilder[AppCtx]) {
//	    comp := clir.WithChildContext(b, resolveComponent)
//	    // comp is *ContextBuilder[*component.Adapter]
//	})
func WithChildContext[T any, U any](
	b *ContextBuilder[T],
	resolve func(parent T, req *Request) (U, error),
) *ContextBuilder[U] {
	return &ContextBuilder[U]{
		base: b.base,
		resolve: func(req *Request) (U, error) {
			parent, err := b.resolve(req)
			if err != nil {
				var zero U
				return zero, err
			}
			return resolve(parent, req)
		},
	}
}

type ParentChild[T any, U any] struct {
	parent T
	child  U
}

func (pc ParentChild[T, U]) Parent() T { return pc.parent }
func (pc ParentChild[T, U]) Child() U  { return pc.child }

func WithParentChildContext[T any, U any](
	b *ContextBuilder[T],
	resolveChild func(parent T, req *Request) (U, error),
) *ContextBuilder[ParentChild[T, U]] {
	return WithChildContext(b, func(parent T, req *Request) (ParentChild[T, U], error) {
		child, err := resolveChild(parent, req)
		if err != nil {
			var zero ParentChild[T, U]
			return zero, err
		}
		return ParentChild[T, U]{parent, child}, nil
	})
}

// WithContextHandler is a convenience that lifts a typed resolver
// and ContextHandler into a plain Handler. This lets you use typed
// contexts even directly with Router.Handle if you don't want the
// builder style.
//
// Example:
//
//	resolveComp := func(req *clir.Request) (*component.Adapter, error) { ... }
//
//	r.Handle("comp <component> image build", "Build images",
//	    clir.WithContextHandler(resolveComp, func(req *clir.Request, c *component.Adapter) error {
//	        return c.BuildImages(req.Context())
//	    }),
//	)
func WithContextHandler[T any](resolve Resolver[T], h ContextHandler[T]) Handler {
	return func(req *Request) error {
		ctxObj, err := resolve(req)
		if err != nil {
			return err
		}
		return h(req, ctxObj)
	}
}
