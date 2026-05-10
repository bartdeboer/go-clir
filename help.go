package clir

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const helpToken = "help"
const helpAllToken = "all"

// RouteInfo is the stable public representation of a route for help/discovery APIs.
type RouteInfo struct {
	Pattern     string
	Description string
	Hidden      bool
	Tags        []string
}

// HasTag reports whether the route has tag.
func (r RouteInfo) HasTag(tag string) bool {
	for _, t := range r.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// HelpOption configures one help rendering call.
type HelpOption func(*helpOptions)

type helpOptions struct {
	includeHidden bool
	filter        func(RouteInfo) bool
}

// IncludeHidden includes routes marked Hidden in help output.
func IncludeHidden() HelpOption {
	return func(opts *helpOptions) {
		opts.includeHidden = true
	}
}

// FilterHelp applies arbitrary consumer-owned filtering to help output.
// It runs after the default hidden-route filter.
func FilterHelp(fn func(RouteInfo) bool) HelpOption {
	return func(opts *helpOptions) {
		opts.filter = fn
	}
}

// HelpEntryFormatter writes the command entries in a help listing.
type HelpEntryFormatter func(w io.Writer, routes []RouteInfo)

// SetHelpEntryFormatter customizes how command entries are written in help output.
// Passing nil restores the default two-column formatter.
func (r *Router) SetHelpEntryFormatter(formatter HelpEntryFormatter) {
	r.helpEntryFormatter = formatter
}

func (r *Router) helpEntryWriter() HelpEntryFormatter {
	if r.helpEntryFormatter == nil {
		return WriteHelpColumns
	}
	return r.helpEntryFormatter
}

func routeInfo(rt *route) RouteInfo {
	return RouteInfo{
		Pattern:     rt.String(),
		Description: rt.desc,
		Hidden:      rt.hidden,
		Tags:        append([]string{}, rt.tags...),
	}
}

// RunWithHelp is the convenience form requested originally.
// It writes managed help to stdout.
func (r *Router) RunWithHelp(ctx context.Context, argv []string) error {
	return r.FRunWithHelp(ctx, os.Stdout, argv)
}

// FRunWithHelp behaves like Run, but if argv ends with "help" or "help all"
// it renders contextual help for that command path instead of running a handler.
func (r *Router) FRunWithHelp(ctx context.Context, w io.Writer, argv []string, opts ...HelpOption) error {
	if w == nil {
		w = os.Stdout
	}

	res, err := r.Resolve(ctx, argv, ResolveHelp())
	if err != nil {
		return err
	}

	switch res.Kind {
	case ResolutionCommand:
		if res.Handler == nil {
			return fmt.Errorf("no matching command for `%s`", strings.Join(argv, " "))
		}
		return res.Handler(res.Request)
	case ResolutionHelp:
		return r.printCommandHelp(w, res.HelpScope, res.HelpAll, makeHelpOptions(opts...))
	default:
		return fmt.Errorf("unsupported resolution kind %q", res.Kind)
	}
}

// FPrintHelp renders contextual help for argv.
// If argv ends with "help" or "help all", those tokens are interpreted as help
// modifiers. Otherwise argv is treated as the command scope.
func (r *Router) FPrintHelp(ctx context.Context, w io.Writer, argv []string, opts ...HelpOption) error {
	_ = ctx
	scope, all, ok := parseHelpRequest(argv)
	if !ok {
		scope = argv
		all = false
	}
	if w == nil {
		w = os.Stdout
	}
	return r.printCommandHelp(w, scope, all, makeHelpOptions(opts...))
}

func parseHelpRequest(argv []string) (scope []string, all bool, ok bool) {
	switch {
	case len(argv) >= 2 && argv[len(argv)-2] == helpToken && argv[len(argv)-1] == helpAllToken:
		return argv[:len(argv)-2], true, true
	case len(argv) >= 1 && argv[len(argv)-1] == helpToken:
		return argv[:len(argv)-1], false, true
	default:
		return nil, false, false
	}
}

// bestHelpRoute resolves an explicit help route for the full argv.
// It only returns routes whose final literal segment is "help".
func (r *Router) bestHelpRoute(argv []string) (*route, bool) {
	bestIdx := -1
	var bestRank uint64

	for i := range r.routes {
		rt := &r.routes[i]

		if !isExplicitHelpRoute(rt) {
			continue
		}

		rank, _ := rt.matchArgv(argv)
		if rank == 0 {
			continue
		}

		if bestIdx == -1 || rank > bestRank {
			bestIdx = i
			bestRank = rank
		}
	}

	if bestIdx == -1 {
		return nil, false
	}
	return &r.routes[bestIdx], true
}

func isExplicitHelpRoute(rt *route) bool {
	if rt == nil || len(rt.segments) == 0 {
		return false
	}
	last := rt.segments[len(rt.segments)-1]
	return last.lit == helpToken
}

func (r *Router) printCommandHelp(w io.Writer, scope []string, all bool, opts helpOptions) error {
	helpRoute, hasHelpRoute := r.bestHelpRoute(append(append([]string{}, scope...), helpToken))
	if !hasHelpRoute {
		helpRoute, hasHelpRoute = r.bestScopeRoute(scope)
	}

	entries := r.helpEntries(scope, all, opts)

	if !hasHelpRoute && len(entries) == 0 {
		return fmt.Errorf("no help available for `%s`", strings.Join(scope, " "))
	}

	if hasHelpRoute && helpRoute.desc != "" {
		fmt.Fprintln(w, helpRoute.desc)
	}

	if len(entries) == 0 {
		return nil
	}

	if hasHelpRoute && helpRoute.desc != "" {
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "Available commands:")
	r.writeHelpEntries(w, entries)
	return nil
}

func (r *Router) bestScopeRoute(scope []string) (*route, bool) {
	rt, req, ok := r.bestMatch(context.Background(), scope)
	if !ok || len(req.Extra) != 0 || rt.handler != nil {
		return nil, false
	}
	return rt, true
}

func (r *Router) helpEntries(scope []string, all bool, opts helpOptions) []RouteInfo {
	descendants := r.descendantRoutes(scope)
	if all {
		return sortedRouteInfos(filterOutHelpRoutes(descendants), opts)
	}
	return consolidateHelpRoutes(scope, descendants, 1, opts)
}

// PrintHelp prints all registered patterns and their descriptions.
func (r *Router) PrintHelp(w io.Writer, opts ...HelpOption) {
	if len(r.routes) == 0 {
		fmt.Fprintln(w, "No commands registered.")
		return
	}

	all := make([]*route, 0, len(r.routes))
	for i := range r.routes {
		all = append(all, &r.routes[i])
	}
	sortRoutesForHelp(all)

	r.writeHelpEntries(w, sortedRouteInfos(all, makeHelpOptions(opts...)))
}

func filterOutHelpRoutes(routes []*route) []*route {
	out := make([]*route, 0, len(routes))
	for _, rt := range routes {
		if !isExplicitHelpRoute(rt) {
			out = append(out, rt)
		}
	}
	return out
}

func consolidateHelpRoutes(scope []string, routes []*route, level int, opts helpOptions) []RouteInfo {
	if level <= 0 {
		return nil
	}

	entryDepth := len(scope) + level
	entries := map[string]*route{}

	for _, rt := range routes {
		if !opts.include(routeInfo(rt)) {
			continue
		}

		relDepth := len(rt.segments) - len(scope)
		if relDepth <= 0 {
			continue
		}

		if isExplicitHelpRoute(rt) {
			if len(rt.segments) == entryDepth+1 {
				entry := routePrefix(rt, entryDepth)
				entry.desc = rt.desc
				key := entry.String()
				existing, ok := entries[key]
				if !ok {
					entries[key] = entry
				} else if existing.desc == "" {
					existing.desc = entry.desc
				}
			}
			continue
		}

		depth := len(rt.segments)
		if relDepth > level {
			depth = entryDepth
		}

		entry := routePrefix(rt, depth)
		if relDepth > level {
			entry.desc = ""
		}

		key := entry.String()
		existing, ok := entries[key]
		if !ok {
			entries[key] = entry
			continue
		}
		if existing.desc == "" && entry.desc != "" {
			existing.desc = entry.desc
		}
	}

	out := make([]*route, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	return sortedRouteInfos(out, opts)
}

func routePrefix(rt *route, n int) *route {
	return &route{
		segments: append([]segment{}, rt.segments[:n]...),
		desc:     rt.desc,
		hidden:   rt.hidden,
		tags:     append([]string{}, rt.tags...),
	}
}

func (r *Router) writeHelpEntries(w io.Writer, entries []RouteInfo) {
	r.helpEntryWriter()(w, entries)
}

func sortedRouteInfos(routes []*route, opts helpOptions) []RouteInfo {
	entries := make([]struct {
		pat     string
		sortPat string
		desc    string
		hidden  bool
		tags    []string
	}, 0, len(routes))

	for _, rt := range routes {
		info := routeInfo(rt)
		if !opts.include(info) {
			continue
		}
		entries = append(entries, struct {
			pat     string
			sortPat string
			desc    string
			hidden  bool
			tags    []string
		}{
			pat:     info.Pattern,
			sortPat: helpSortKey(rt),
			desc:    info.Description,
			hidden:  info.Hidden,
			tags:    info.Tags,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].sortPat != entries[j].sortPat {
			return entries[i].sortPat < entries[j].sortPat
		}
		return entries[i].pat < entries[j].pat
	})

	out := make([]RouteInfo, len(entries))
	for i, e := range entries {
		out[i] = RouteInfo{
			Pattern:     e.pat,
			Description: e.desc,
			Hidden:      e.hidden,
			Tags:        append([]string{}, e.tags...),
		}
	}
	return out
}

func makeHelpOptions(opts ...HelpOption) helpOptions {
	var out helpOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

func (opts helpOptions) include(info RouteInfo) bool {
	if info.Hidden && !opts.includeHidden {
		return false
	}
	if opts.filter != nil && !opts.filter(info) {
		return false
	}
	return true
}

// WriteHelpColumns writes command entries as the default aligned two-column list.
func WriteHelpColumns(w io.Writer, routes []RouteInfo) {
	maxLen := 0
	for _, e := range routes {
		if l := len(e.Pattern); l > maxLen {
			maxLen = l
		}
	}

	format := fmt.Sprintf("  %%-%ds  %%s\n", maxLen)
	for _, e := range routes {
		if e.Description == "" {
			fmt.Fprintf(w, "  %s\n", e.Pattern)
			continue
		}
		fmt.Fprintf(w, format, e.Pattern, e.Description)
	}
}

// WriteHelpInline writes command entries as "<pattern> - <description>" lines.
func WriteHelpInline(w io.Writer, routes []RouteInfo) {
	for _, e := range routes {
		if e.Description == "" {
			fmt.Fprintln(w, e.Pattern)
			continue
		}
		fmt.Fprintf(w, "%s - %s\n", e.Pattern, e.Description)
	}
}

func sortRoutesForHelp(routes []*route) {
	sort.Slice(routes, func(i, j int) bool {
		ki := helpSortKey(routes[i])
		kj := helpSortKey(routes[j])
		if ki != kj {
			return ki < kj
		}
		return routes[i].String() < routes[j].String()
	})
}

func helpSortKey(rt *route) string {
	var parts []string
	for _, s := range rt.segments {
		if s.lit != "" {
			parts = append(parts, fmt.Sprintf("%04d %s", s.sort, s.lit))
		}
	}
	return strings.Join(parts, " ")
}
