package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	dump := flag.Bool("dump", false, "print the canvas as plain text and exit (debug)")
	flag.Parse()

	if *dump {
		cs, err := fetchContainers()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		links := buildLinks(cs)
		L := computeLayout(cs, links, zoomDims[defaultZoom])
		g := L.paint("", true)
		for y := 0; y < g.h; y++ {
			var sb strings.Builder
			for x := 0; x < g.w; x++ {
				r := g.at(x, y).r
				if r == 0 {
					r = ' '
				}
				sb.WriteRune(r)
			}
			fmt.Println(strings.TrimRight(sb.String(), " "))
		}
		for _, l := range links {
			fmt.Printf("%s -> %s via=%v ports=%v keys=%v\n", L.Nodes[l.From].C.Name, L.Nodes[l.To].C.Name, l.ViaHost, l.Ports, l.Keys)
		}
		return
	}

	m := newModel()
	prog = tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
