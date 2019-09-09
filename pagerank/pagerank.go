//package pagerank implements the *weighted* PageRank algorithm.
// forked from github.com/alixaxel/pagerank/pagerank.go
package pagerank

import (
	"hash/crc32"
	"log"
	"math"
	"net/url"
	"sync"
)

type Page struct {
	weight   float64
	outbound float64

	addr url.URL
}

func (p *Page) Priority() float64 { return p.weight }
func (p *Page) ID() uint32        { return urlsum(p.addr) }
func (p *Page) URL() *url.URL     { return &p.addr }
func (p *Page) Links(to *Page)    {}

// Graph holds node and edge data.
type Graph struct {
	sync.Mutex

	edges map[uint32](map[uint32]float64)
	nodes map[uint32]*Page
}

// NewGraph initializes and returns a new graph.
func NewGraph() *Graph {
	return &Graph{
		edges: make(map[uint32](map[uint32]float64)),
		nodes: make(map[uint32]*Page),
	}
}

// Reset clears all the current graph data.
func (g *Graph) Reset() {
	g.edges = make(map[uint32](map[uint32]float64))
	g.nodes = make(map[uint32]*Page)
}

func (g *Graph) Get(addr *url.URL) *Page {
	g.Lock()
	defer g.Unlock()
	sum := urlsum(*addr)
	n, ok := g.nodes[sum]
	if !ok {
		return nil
	}
	return n
}

// Add is a no-op if the page already exists
func (g *Graph) Add(addr *url.URL) {
	a := *addr
	sum := urlsum(a)
	g.Lock()
	defer g.Unlock()
	if _, ok := g.nodes[sum]; !ok {
		g.nodes[sum] = &Page{
			weight:   0,
			outbound: 0,
			addr:     a,
		}
	}
}

func urlsum(u url.URL) uint32 {
	x := u
	x.RawQuery = ""
	return crc32.ChecksumIEEE([]byte(x.String()))
}

// Link creates a weighted edge between a source-target node pair.
func (g *Graph) Link(sourceURL, targetURL *url.URL, weight float64) {
	log.Printf("attempting %s > %s\n", sourceURL, targetURL)

	source := g.Get(sourceURL)
	target := g.Get(targetURL)

	if source == nil {
		log.Printf("node %s doesn't exist\n", sourceURL)
		return
	}
	source.outbound += weight

	if target == nil {
		log.Printf("node %s doesn't exist\n", targetURL)
		return
	}

	if source.ID() == target.ID() {
		log.Printf("source target are the same %s %x > %s %x \n", sourceURL, source.ID(), targetURL, target.ID())
		return
	}

	g.Lock()
	if _, ok := g.edges[source.ID()]; ok == false {
		g.edges[source.ID()] = map[uint32]float64{}
	}
	g.edges[source.ID()][target.ID()] += weight
	g.Unlock()

	log.Printf("linked %s > %s\n", sourceURL, targetURL)

}

// Rank computes the PageRank of every node in the directed graph.
// α (alpha) is the damping factor, usually set to 0.85.
// ε (epsilon) is the convergence criteria, usually set to a tiny value.
//
// This method will run as many iterations as needed, until the graph converges.
func (g *Graph) Rank(α, ε float64) {
	g.Lock()
	defer g.Unlock()

	Δ := float64(1.0)
	inverse := 1 / float64(len(g.nodes))

	// Normalize all the edge weights so that their sum amounts to 1.
	for source := range g.edges {
		if g.nodes[source].outbound > 0 {
			for target := range g.edges[source] {
				g.edges[source][target] /= g.nodes[source].outbound
			}
		}
	}

	for key := range g.nodes {
		g.nodes[key].weight = inverse
	}

	for Δ > ε {
		leak := float64(0)
		nodes := map[uint32]float64{}

		for key, value := range g.nodes {
			nodes[key] = value.weight

			if value.outbound == 0 {
				leak += value.weight
			}

			g.nodes[key].weight = 0
		}

		leak *= α

		for source := range g.nodes {
			for target, weight := range g.edges[source] {
				g.nodes[target].weight += α * nodes[source] * weight
			}

			g.nodes[source].weight += (1-α)*inverse + leak*inverse
		}

		Δ = 0

		for key, value := range g.nodes {
			Δ += math.Abs(value.weight - nodes[key])
		}
	}

}

func (g *Graph) Traverse(u url.URL, seen []uint32, depth int, callback func(string, string, float64)) {
	if depth <= 0 {
		return
	}
	source := g.Get(&u)
	if source == nil {
		return
	}

	from := source.ID()
	for to, weight := range g.edges[from] {
		for _, v := range seen {
			if v == to {
				return
			}
		}
		target := g.nodes[to]
		g.Traverse(target.addr, append(seen, from, to), depth-1, callback)
		callback(source.addr.String(), target.addr.String(), weight)
	}
}
func (g *Graph) Walk(callback func(addr string, rank float64)) {
	for _, page := range g.nodes {
		callback(page.addr.String(), page.weight)
	}
}