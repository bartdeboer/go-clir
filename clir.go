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

// ResolutionKind describes what Router.Resolve found for argv.
type ResolutionKind string

const (
	// ResolutionCommand means argv resolved to an executable command route.
	ResolutionCommand ResolutionKind = "command"

	// ResolutionHelp means argv resolved to contextual help.
	ResolutionHelp ResolutionKind = "help"
)

// Resolution is the result of resolving argv.
type Resolution struct {
	Kind ResolutionKind

	// Request is set for command resolutions.
	Request *Request
	Handler Handler

	// HelpScope and HelpAll are set for help resolutions.
	HelpScope []string
	HelpAll   bool

	route *route
}

// ResolveOption configures one Resolve call.
type ResolveOption func(*resolveOptions)

type resolveOptions struct {
	help bool
}

// ResolveHelp allows argv ending in "help" or "help all" to resolve as help.
func ResolveHelp() ResolveOption {
	return func(opts *resolveOptions) {
		opts.help = true
	}
}

type segment struct {
	lit   string // non-empty for static segment: "comp", "image", "build"
	param string // non-empty for param segment: e.g. "component" for "<component>"
	sort  int    // optional sort/level hint derived from numeric prefixes
}

type route struct {
	segments []segment
	handler  Handler
	desc     string
	hidden   bool
	tags     []string
}

// RouteOption configures route metadata used by help/discovery APIs.
type RouteOption func(*route)

// Hidden marks a route as hidden from help/discovery output by default.
// Hidden routes still match and run normally.
func Hidden() RouteOption {
	return func(rt *route) {
		rt.hidden = true
	}
}

// Tag attaches generic route tags for consumers that want filtered help views.
func Tag(tags ...string) RouteOption {
	return func(rt *route) {
		rt.tags = append(rt.tags, tags...)
	}
}

// Router holds all registered routes and can execute them for argv.
type Router struct {
	routes             []route
	helpEntryFormatter HelpEntryFormatter
}

// New creates an empty Router.
func New() *Router {
	return &Router{}
}

func (rt *route) String() string {
	var b strings.Builder
	for i, s := range rt.segments {
		if i > 0 {
			b.WriteByte(' ')
		}
		switch {
		case s.lit != "":
			b.WriteString(s.lit)
		case s.param != "":
			b.WriteByte('<')
			b.WriteString(s.param)
			b.WriteByte('>')
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
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
func (r *Router) Handle(pattern, desc string, h Handler, opts ...RouteOption) {
	r.addRoute(pattern, desc, h, opts...)
}

// Group registers a non-executable route that contributes metadata to help.
func (r *Router) Group(pattern, desc string, opts ...RouteOption) {
	r.addRoute(pattern, desc, nil, opts...)
}

func (r *Router) addRoute(pattern, desc string, h Handler, opts ...RouteOption) {
	parts := strings.Fields(pattern)
	segs := parseSegments(parts)

	rt := route{
		segments: segs,
		handler:  h,
		desc:     desc,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&rt)
		}
	}

	r.routes = append(r.routes, rt)
}

// 2 bits per segment, left-to-right => early tokens dominate.
// Max 32 segments if using uint64 (2*32 = 64).
// matchRank returns a 2-bit-per-segment rank built left->right (early tokens dominate).
// Encoding:
//
//	10 = literal match
//	01 = param match
//
// With this encoding, longer matches always rank higher than shorter matches (since codes are non-zero).
// Uses uint64 => max 32 segments.
func (rt *route) matchArgv(argv []string) (rank uint64, params Params) {
	segs := rt.segments
	if len(argv) < len(segs) {
		return 0, nil
	}
	if len(segs) > 32 {
		return 0, nil
	}

	params = Params{}
	for i, s := range segs {
		arg := argv[i]

		var code uint64
		switch {
		case s.lit != "":
			if arg != s.lit {
				return 0, nil
			}
			code = 0b10
		case s.param != "":
			params[s.param] = arg
			code = 0b01
		default:
			return 0, nil
		}

		// rank = (rank << 2) | code // Right-left LSB-first placement (longest wins)
		shift := uint(2 * (32 - 1 - i)) // Left-right MSB-first placement (literal wins)
		rank |= code << shift

	}

	return rank, params
}

// bestMatch finds the best matching route by highest rank.
// Returns (routePtr, reqPtr, ok).
func (r *Router) bestMatch(ctx context.Context, argv []string) (*route, *Request, bool) {
	if ctx == nil {
		ctx = context.Background()
	}

	bestIdx := -1
	var bestRank uint64
	var bestParams Params
	var bestExtra []string

	for i := range r.routes {
		rt := &r.routes[i]

		rank, params := rt.matchArgv(argv)
		if rank == 0 {
			continue
		}

		if bestIdx == -1 || rank > bestRank {
			bestIdx = i
			bestRank = rank
			bestParams = params
			bestExtra = argv[len(rt.segments):]
		}
	}

	if bestIdx == -1 {
		return nil, nil, false
	}

	req := &Request{
		ctx:    ctx,
		Args:   argv,
		Params: bestParams,
		Extra:  bestExtra,
	}
	return &r.routes[bestIdx], req, true
}

func makeResolveOptions(opts ...ResolveOption) resolveOptions {
	var out resolveOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

// Resolve resolves argv to either an executable command or contextual help.
func (r *Router) Resolve(ctx context.Context, argv []string, opts ...ResolveOption) (Resolution, error) {
	options := makeResolveOptions(opts...)
	rt, req, matched := r.bestMatch(ctx, argv)

	if options.help {
		scope, all, ok := parseHelpRequest(argv)
		if ok {
			if matched &&
				len(req.Extra) == 0 &&
				isExplicitHelpRoute(rt) &&
				rt.handler != nil {
				return Resolution{
					Kind:    ResolutionCommand,
					Request: req,
					Handler: rt.handler,
					route:   rt,
				}, nil
			}

			return Resolution{
				Kind:      ResolutionHelp,
				HelpScope: append([]string{}, scope...),
				HelpAll:   all,
			}, nil
		}
	}

	if !matched {
		return Resolution{}, fmt.Errorf("no matching command for `%s`", strings.Join(argv, " "))
	}
	if rt.handler == nil {
		return Resolution{}, fmt.Errorf("command `%s` is not executable", strings.Join(argv, " "))
	}

	return Resolution{
		Kind:    ResolutionCommand,
		Request: req,
		Handler: rt.handler,
		route:   rt,
	}, nil
}

func (rt *route) matchPrefix(argv []string) (rank uint64, params Params) {
	if len(argv) == 0 {
		return 1, Params{}
	}
	if len(argv) > len(rt.segments) {
		return 0, nil
	}
	if len(argv) > 32 {
		return 0, nil
	}

	params = Params{}
	for i, arg := range argv {
		s := rt.segments[i]

		var code uint64
		switch {
		case s.lit != "":
			if arg != s.lit {
				return 0, nil
			}
			code = 0b10
		case s.param != "":
			params[s.param] = arg
			code = 0b01
		default:
			return 0, nil
		}

		shift := uint(2 * (32 - 1 - i))
		rank |= code << shift
	}

	return rank, params
}

func (r *Router) descendantRoutes(argv []string) []*route {
	var out []*route
	var bestRank uint64

	for i := range r.routes {
		rt := &r.routes[i]

		rank, _ := rt.matchPrefix(argv)
		if rank == 0 {
			continue
		}

		if len(rt.segments) <= len(argv) {
			continue
		}

		if bestRank == 0 || rank > bestRank {
			bestRank = rank
			out = out[:0]
		}
		if rank == bestRank {
			out = append(out, rt)
		}
	}

	sortRoutesForHelp(out)
	return out
}

// Run attempts to match argv against registered routes and executes
// the first matching handler. ctx becomes the root context for the Request.
func (r *Router) Run(ctx context.Context, argv []string) error {
	res, err := r.Resolve(ctx, argv)
	if err != nil {
		return err
	}
	if res.Kind != ResolutionCommand || res.Handler == nil {
		return fmt.Errorf("no matching command for `%s`", strings.Join(argv, " "))
	}
	return res.Handler(res.Request)
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
func (b *Builder) Handle(path, desc string, h Handler, opts ...RouteOption) {
	parts := strings.Fields(path)
	full := append(append([]string{}, b.prefix...), parts...)
	pattern := strings.Join(full, " ")

	// Apply middleware chain (outermost first).
	wrapped := h
	for i := len(b.mws) - 1; i >= 0; i-- {
		wrapped = b.mws[i](wrapped)
	}

	b.router.Handle(pattern, desc, wrapped, opts...)
}

// Group registers a non-executable route under the current prefix.
func (b *Builder) Group(path, desc string, opts ...RouteOption) {
	parts := strings.Fields(path)
	full := append(append([]string{}, b.prefix...), parts...)
	pattern := strings.Join(full, " ")
	b.router.Group(pattern, desc, opts...)
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
func (b *ContextBuilder[T]) Handle(path, desc string, h ContextHandler[T], opts ...RouteOption) {
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

	b.base.router.Handle(pattern, desc, wrapped, opts...)
}

// Group registers a non-executable route under the current typed prefix.
func (b *ContextBuilder[T]) Group(path, desc string, opts ...RouteOption) {
	b.base.Group(path, desc, opts...)
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

// This doesn't need validation. The resolver can return an error for that.
// func (pc ParentChild[T, U]) Validate() error { return nil }

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
