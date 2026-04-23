package clir

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// prefixMatchRoutes returns all routes where argv matches the route prefix
// and the route has exactly len(argv)+extraSegments segments.
func (r *Router) prefixMatchRoutes(argv []string, extraSegments int) []route {
	if extraSegments < 0 {
		return nil
	}

	wantLen := len(argv) + extraSegments
	matches := make([]route, 0)

	for _, rt := range r.routes {
		if len(rt.segments) != wantLen {
			continue
		}

		if !rt.matchArgvPrefix(argv) {
			continue
		}

		matches = append(matches, rt)
	}

	return matches
}

func (rt *route) matchArgvPrefix(argv []string) bool {
	if len(argv) > len(rt.segments) {
		return false
	}

	for i, arg := range argv {
		seg := rt.segments[i]
		switch {
		case seg.lit != "":
			if arg != seg.lit {
				return false
			}
		case seg.param != "":
			continue
		default:
			return false
		}
	}

	return true
}

// RunWithHelp works like Run, but if argv ends with "help" it renders
// contextual help for that command path instead of running a handler.
func (r *Router) RunWithHelp(ctx context.Context, argv []string, w io.Writer) error {
	if len(argv) == 0 || argv[len(argv)-1] != "help" {
		return r.Run(ctx, argv)
	}

	if w == nil {
		w = os.Stdout
	}

	return r.printCommandHelp(w, argv)
}

func (r *Router) printCommandHelp(w io.Writer, argv []string) error {
	helpRoute, hasHelpRoute := r.bestPrefixMatchRoute(argv, 0)
	query := argv[:len(argv)-1]
	descendants := filterHelpRoutes(r.prefixMatchRoutes(query, 1))

	if !hasHelpRoute && len(descendants) == 0 {
		return fmt.Errorf("no help available for `%s`", strings.Join(query, " "))
	}

	if hasHelpRoute && helpRoute.desc != "" {
		fmt.Fprintln(w, helpRoute.desc)
	}

	if len(descendants) == 0 {
		return nil
	}

	if hasHelpRoute && helpRoute.desc != "" {
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "Available commands:")
	writeHelpEntries(w, descendants)
	return nil
}

func (r *Router) bestPrefixMatchRoute(argv []string, extraSegments int) (*route, bool) {
	matches := r.prefixMatchRoutes(argv, extraSegments)
	if len(matches) == 0 {
		return nil, false
	}

	bestIdx := 0
	bestRank, _ := matches[0].matchArgv(argv)

	for i := 1; i < len(matches); i++ {
		rank, _ := matches[i].matchArgv(argv)
		if rank > bestRank {
			bestIdx = i
			bestRank = rank
		}
	}

	return &matches[bestIdx], true
}

// PrintHelp prints all registered patterns and their descriptions,
// sorted alphabetically by pattern.
func (r *Router) PrintHelp(w io.Writer) {
	if len(r.routes) == 0 {
		fmt.Fprintln(w, "No commands registered.")
		return
	}

	fmt.Fprintln(w, "Available commands:")
	writeHelpEntries(w, r.routes)
}

func writeHelpEntries(w io.Writer, routes []route) {
	entries := make([]struct {
		pat     string
		sortPat string
		desc    string
	}, len(routes))

	for i, rt := range routes {
		var sortParts []string
		for _, s := range rt.segments {
			if s.lit != "" {
				sortParts = append(sortParts, fmt.Sprintf("%d %s", s.sort, s.lit))
			}
		}
		entries[i].pat = rt.String()
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

	format := fmt.Sprintf("  %%-%ds  %%s\n", maxLen)
	for _, e := range entries {
		fmt.Fprintf(w, format, e.pat, e.desc)
	}
}

func filterHelpRoutes(routes []route) []route {
	filtered := make([]route, 0, len(routes))
	for _, rt := range routes {
		if len(rt.segments) == 0 {
			continue
		}

		last := rt.segments[len(rt.segments)-1]
		if last.lit == "help" {
			continue
		}

		filtered = append(filtered, rt)
	}

	return filtered
}
