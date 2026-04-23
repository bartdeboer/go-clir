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

// RunWithHelp is the convenience form requested originally.
// It writes managed help to stdout.
func (r *Router) RunWithHelp(ctx context.Context, argv []string) error {
	return r.RunWithHelpTo(ctx, argv, os.Stdout)
}

// RunWithHelpTo behaves like Run, but if argv ends with "help" it renders
// contextual help for that command path instead of running a handler.
func (r *Router) RunWithHelpTo(ctx context.Context, argv []string, w io.Writer) error {
	if len(argv) == 0 || argv[len(argv)-1] != helpToken {
		return r.Run(ctx, argv)
	}
	if w == nil {
		w = os.Stdout
	}
	return r.printCommandHelp(w, argv)
}

// prefixMatchRoutesAtDepth returns all routes where argv matches the route prefix
// and the route has exactly len(argv)+extraSegments segments.
func (r *Router) prefixMatchRoutesAtDepth(argv []string, extraSegments int) []*route {
	if extraSegments < 0 {
		return nil
	}

	wantLen := len(argv) + extraSegments
	matches := make([]*route, 0)

	for i := range r.routes {
		rt := &r.routes[i]

		if len(rt.segments) != wantLen {
			continue
		}
		if rank, _ := rt.matchPrefix(argv); rank == 0 {
			continue
		}

		matches = append(matches, rt)
	}

	sortRoutesForHelp(matches)
	return matches
}

// filterDepthRoutes returns all routes under argv up to maxDepth extra levels.
//
// maxDepth == 0 => exact scope only
// maxDepth == 1 => children
// maxDepth == 2 => children + grandchildren
func (r *Router) filterDepthRoutes(argv []string, maxDepth int) []*route {
	if maxDepth < 0 {
		return nil
	}

	matches := make([]*route, 0)

	for i := range r.routes {
		rt := &r.routes[i]

		relDepth := len(rt.segments) - len(argv)
		if relDepth < 0 || relDepth > maxDepth {
			continue
		}
		if rank, _ := rt.matchPrefix(argv); rank == 0 {
			continue
		}

		matches = append(matches, rt)
	}

	sortRoutesForHelp(matches)
	return matches
}

// bestHelpRoute resolves an explicit help route for the full argv.
// It only returns routes whose final literal segment is "help".
func (r *Router) bestHelpRoute(argv []string) (*route, bool) {
	matches := r.prefixMatchRoutesAtDepth(argv, 0)
	if len(matches) == 0 {
		return nil, false
	}

	var best *route
	var bestRank uint64

	for _, rt := range matches {
		if !isExplicitHelpRoute(rt) {
			continue
		}

		rank, _ := rt.matchPrefix(argv)
		if best == nil || rank > bestRank {
			best = rt
			bestRank = rank
		}
	}

	if best == nil {
		return nil, false
	}
	return best, true
}

func isExplicitHelpRoute(rt *route) bool {
	if rt == nil || len(rt.segments) == 0 {
		return false
	}
	last := rt.segments[len(rt.segments)-1]
	return last.lit == helpToken
}

func (r *Router) printCommandHelp(w io.Writer, argv []string) error {
	helpRoute, hasHelpRoute := r.bestHelpRoute(argv)
	scope := argv[:len(argv)-1]

	// Managed help shows immediate children by default.
	children := filterOutHelpRoutes(r.prefixMatchRoutesAtDepth(scope, 1))

	if !hasHelpRoute && len(children) == 0 {
		return fmt.Errorf("no help available for `%s`", strings.Join(scope, " "))
	}

	if hasHelpRoute && helpRoute.desc != "" {
		fmt.Fprintln(w, helpRoute.desc)
	}

	if len(children) == 0 {
		return nil
	}

	if hasHelpRoute && helpRoute.desc != "" {
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "Available commands:")
	writeHelpEntries(w, children)
	return nil
}

// PrintHelp prints all registered patterns and their descriptions.
func (r *Router) PrintHelp(w io.Writer) {
	if len(r.routes) == 0 {
		fmt.Fprintln(w, "No commands registered.")
		return
	}

	all := make([]*route, 0, len(r.routes))
	for i := range r.routes {
		all = append(all, &r.routes[i])
	}
	sortRoutesForHelp(all)

	fmt.Fprintln(w, "Available commands:")
	writeHelpEntries(w, all)
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

func writeHelpEntries(w io.Writer, routes []*route) {
	entries := make([]struct {
		pat     string
		sortPat string
		desc    string
	}, len(routes))

	for i, rt := range routes {
		entries[i].pat = rt.String()
		entries[i].sortPat = helpSortKey(rt)
		entries[i].desc = rt.desc
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].sortPat != entries[j].sortPat {
			return entries[i].sortPat < entries[j].sortPat
		}
		return entries[i].pat < entries[j].pat
	})

	maxLen := 0
	for _, e := range entries {
		if l := len(e.pat); l > maxLen {
			maxLen = l
		}
	}

	format := fmt.Sprintf("  %%-%ds  %%s\n", maxLen)
	for _, e := range entries {
		fmt.Fprintf(w, format, e.pat, e.desc)
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
