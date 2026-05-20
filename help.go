package clir

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

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
	depth         *int
	litDepth      *int
}

// IsHelpRequest reports whether argv ends with clir's built-in help token
// convention: "help", "--help", or "—help".
func IsHelpRequest(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	return isHelpToken(argv[len(argv)-1])
}

// StripHelpToken returns argv without a trailing clir help token. If argv does
// not end with a help token, StripHelpToken returns a copy of argv unchanged.
func StripHelpToken(argv []string) []string {
	if !IsHelpRequest(argv) {
		return append([]string{}, argv...)
	}
	return append([]string{}, argv[:len(argv)-1]...)
}

func isHelpToken(arg string) bool {
	switch arg {
	// Some chat/mobile inputs render --help with an em dash. Accepting it here
	// keeps help detection forgiving without changing route matching itself.
	case "help", "--help", "—help":
		return true
	default:
		return false
	}
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

// Depth limits contextual help to n route segments below the help scope.
func Depth(n int) HelpOption {
	return func(opts *helpOptions) {
		opts.depth = &n
	}
}

// LitDepth limits contextual help to n literal route segments below the help
// scope. Parameter segments do not count toward the limit.
func LitDepth(n int) HelpOption {
	return func(opts *helpOptions) {
		opts.litDepth = &n
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

// FPrintHelp renders contextual help for argv.
// argv is treated exactly as the command scope; it is not parsed for trailing
// help tokens.
func (r *Router) FPrintHelp(ctx context.Context, w io.Writer, argv []string, opts ...HelpOption) error {
	_ = ctx
	if w == nil {
		w = os.Stdout
	}
	return r.printCommandHelp(w, append([]string{}, argv...), makeHelpOptions(opts...))
}

func (r *Router) printCommandHelp(w io.Writer, scope []string, opts helpOptions) error {
	helpRoute, hasHelpRoute := r.bestScopeRoute(scope)
	entries := r.helpEntries(scope, opts)

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
	bestIdx := -1
	var bestRank uint64

	for i := range r.routes {
		rt := &r.routes[i]
		if rt.handler != nil || len(rt.segments) != len(scope) {
			continue
		}

		rank := uint64(1)
		if len(scope) > 0 {
			rank, _ = rt.matchArgv(scope)
			if rank == 0 {
				continue
			}
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

func (r *Router) helpEntries(scope []string, opts helpOptions) []RouteInfo {
	descendants := r.descendantRoutes(scope)
	if !opts.hasDepthLimit() {
		return sortedRouteInfos(uniqueHelpRoutes(descendants), opts)
	}
	return consolidateHelpRoutes(scope, descendants, opts)
}

func uniqueHelpRoutes(routes []*route) []*route {
	entries := map[string]*route{}

	for _, rt := range routes {
		key := rt.String()
		existing, ok := entries[key]
		if !ok {
			entries[key] = rt
			continue
		}
		if existing.handler == nil && rt.handler != nil {
			entries[key] = rt
			continue
		}
		if existing.desc == "" && rt.desc != "" {
			cp := *existing
			cp.desc = rt.desc
			entries[key] = &cp
		}
	}

	out := make([]*route, 0, len(entries))
	for _, rt := range entries {
		out = append(out, rt)
	}
	return out
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

func consolidateHelpRoutes(scope []string, routes []*route, opts helpOptions) []RouteInfo {
	entries := map[string]*route{}

	for _, rt := range routes {
		if !opts.include(routeInfo(rt)) {
			continue
		}

		relDepth := len(rt.segments) - len(scope)
		if relDepth <= 0 {
			continue
		}

		entry := helpEntryRoute(scope, rt, opts)
		if entry == nil {
			continue
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

func helpEntryRoute(scope []string, rt *route, opts helpOptions) *route {
	segs := rt.segments
	desc := rt.desc

	if len(segs) <= len(scope) {
		return nil
	}

	depth := len(segs)
	truncated := false

	if opts.depth != nil {
		if *opts.depth <= 0 {
			return nil
		}
		maxDepth := len(scope) + *opts.depth
		if depth > maxDepth {
			depth = maxDepth
			truncated = true
		}
	}

	if opts.litDepth != nil {
		if *opts.litDepth <= 0 {
			return nil
		}
		litDepth := prefixDepthForLiteralLimit(scope, segs, *opts.litDepth)
		if litDepth < depth {
			depth = litDepth
			truncated = true
		}
	}

	entry := &route{
		segments: append([]segment{}, segs[:depth]...),
		desc:     desc,
		hidden:   rt.hidden,
		tags:     append([]string{}, rt.tags...),
	}
	if truncated {
		entry.desc = ""
	}
	return entry
}

func prefixDepthForLiteralLimit(scope []string, segs []segment, limit int) int {
	literals := 0
	for i := len(scope); i < len(segs); i++ {
		if segs[i].lit == "" {
			continue
		}
		literals++
		if literals == limit {
			depth := i + 1
			for depth < len(segs) && segs[depth].param != "" {
				depth++
			}
			return depth
		}
	}
	return len(segs)
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

func (opts helpOptions) hasDepthLimit() bool {
	return opts.depth != nil || opts.litDepth != nil
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
