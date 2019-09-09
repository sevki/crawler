package crawler

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/temoto/robotstxt"
	"golang.org/x/net/html"
	"sevki.org/crawler/pagerank"
	"sevki.org/crawler/pqueue"
)

const ua = "Sevki's Crawler/0.1 (+https://sevki.io/bot.html)"

type robots struct {
	sync.Mutex

	robots map[string]*robotstxt.RobotsData
}

func (r *robots) FromResponse(res *http.Response) {
	r.Lock()
	r.robots[res.Request.URL.Host], _ = robotstxt.FromResponse(res)
	r.Unlock()
}

func (r *robots) Test(u *url.URL) bool {
	r.Lock()
	r.Unlock()
	d, ok := r.robots[u.Host]
	if !ok { // there doesn't seem to be robots data
		return true
	}

	return d.TestAgent(u.Path, ua)
}

type Crawler struct {
	// This is an abstraction for PUB/SUB or SQS
	q *pqueue.PQueue
	// This is an abstraction for for a proper graph database like dgraph or neojs
	g *pagerank.Graph
	// this should probably be centralized with the rest of this graph
	r *robots

	c       *http.Client
	timeout time.Duration
	skip    func(*url.URL) bool
	α, ε    float64
	mux     *http.ServeMux
}

func (c *Crawler) ServeHTTP(w http.ResponseWriter, r *http.Request) { c.mux.ServeHTTP(w, r) }

type uaRoundtripper struct {
	ua string
	t  http.RoundTripper
}

func (t uaRoundtripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("User-Agent", t.ua)
	return t.t.RoundTrip(r)
}

func New(client *http.Client, workers int, skipFn func(*url.URL) bool) *Crawler {
	if client == nil {
		client = http.DefaultClient
	}
	if client.Transport == nil {
		client.Transport = http.DefaultTransport
	}
	client.Transport = uaRoundtripper{ua, client.Transport} // wrap the transport to make sure the ua is set
	c := &Crawler{
		c:       client,
		q:       pqueue.New(),
		r:       &robots{robots: make(map[string]*robotstxt.RobotsData)},
		timeout: time.Second * 10,
		g:       pagerank.NewGraph(),
		α:       .85,
		ε:       .0001,
		mux:     http.NewServeMux(),
		skip:    skipFn,
	}
	c.mux.HandleFunc("/map", c.sitemap)
	c.mux.HandleFunc("/crawl", c.crawlhandler)
	c.mux.HandleFunc("/index", func(w http.ResponseWriter, r *http.Request) {
		c.g.Walk(func(node string, rank float64) {
			fmt.Fprintln(w, "Node", node, "has a rank of", rank)
		})
	})

	for i := 0; i < workers; i++ {
		go c.work()
	}
	return c
}

// this isn't being
func (c *Crawler) loadRobots(s string) {
	u, err := url.Parse(s)
	if err != nil {
		return
	}
	u.Path = "robots.txt"
	resp, err := c.c.Get(u.String())
	if err != nil {
		return
	}
	c.r.FromResponse(resp)

}
func (c *Crawler) crawlSite(s string) {
	c.g.Reset()
	c.loadRobots(s) // load robots data here as this is the entry point
	c.Crawl(s)
	c.q.Wait()
	c.g.Rank(c.α, c.ε)
}

func (c *Crawler) crawlhandler(w http.ResponseWriter, r *http.Request) {
	s := "https://" + r.URL.RawQuery
	c.crawlSite(s)
}

const tpl = `<html>

  <head>
    <title>cytoscape-dagre.js demo</title>

    <meta name="viewport" content="width=device-width, user-scalable=no, initial-scale=1, maximum-scale=1">

    <script src="https://unpkg.com/cytoscape/dist/cytoscape.min.js"></script>

    <!-- for testing with local version of cytoscape.js -->
    <!--<script src="../cytoscape.js/build/cytoscape.js"></script>-->

    <script src="https://unpkg.com/dagre@0.7.4/dist/dagre.js"></script>
    <script src="https://cytoscape.org/cytoscape.js-dagre/cytoscape-dagre.js"></script>

    <style>
      body {
        font-family: helvetica;
        font-size: 14px;
      }

      #cy {
        width: 100%;
        height: 100%;
        position: absolute;
        left: 0;
        top: 0;
        z-index: 999;
      }

      h1 {
        opacity: 0.5;
        font-size: 1em;
      }
    </style>

    <script>
      window.addEventListener('DOMContentLoaded', function(){

        var cy = window.cy = cytoscape({
          container: document.getElementById('cy'),

          boxSelectionEnabled: false,
          autounselectify: true,

          layout: {
            name: 'dagre'
          },

          style: [
            {
              selector: 'node',
              style: {
                'background-color': '#11479e',
                'label': 'data(id)'
              }
            },

            {
              selector: 'edge',
              style: {
                'width': 4,
                'target-arrow-shape': 'triangle',
                'line-color': '#9dbaea',
                'target-arrow-color': '#9dbaea',
                'curve-style': 'bezier'
              }
            }
          ],

          elements: {
            nodes: [
{{range . -}}
	{ data: { id: '{{ .To }}' } },
	{ data: { id: '{{ .From }}' } },
{{ end}}
            ],
            edges: [
{{range . -}}
	{ data: { source: '{{ .From }}', target: '{{ .To }}' } },
{{ end}}
            ]
          }
        });

      });
    </script>
  </head>

  <body>
    <h1>cytoscape-dagre demo</h1>

    <div id="cy"></div>

  </body>

</html>`

// This is for testing purposes
func (c *Crawler) sitemap(w http.ResponseWriter, r *http.Request) {
	check := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	t, err := template.New("webpage").Parse(tpl)
	check(err)

	type edge struct {
		From, To string
		Weight   float64
	}
	data := []edge{}

	s := "https://" + r.URL.RawQuery

	u, err := url.Parse(s)

	// only traverse this 3 levels deep
	c.g.Traverse(*u, nil, 2, func(source, target string, weight float64) {
		data = append(data, edge{source, target, weight})
	})

	err = t.Execute(w, data)
	check(err)
}

func (c *Crawler) Crawl(s string) {
	u, err := url.Parse(s)
	if err != nil {
		return
	}
	if c.skip(u) {
		return
	}
	if p := c.g.Get(u); p != nil {
		// we've already indexed this, let's move on
		return
	}

	c.g.Add(u)
	c.q.Push(c.g.Get(u))
}

func (c *Crawler) Index(p *pagerank.Page) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	req, err := http.NewRequest(http.MethodGet, p.URL().String(), nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	resp, err := c.c.Do(req)
	if err != nil {
		return err
	}

	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return nil
	}

	links, err := processpage(ctx, *p.URL(), resp.Body, c)
	cancel()
	resp.Body.Close()
	if err != nil {
		return err
	}

	for _, l := range links {
		x := *l
		x.Fragment = "" // we'll remove the fragment as we're not running any js code which means it'll always be the same page
		c.Crawl(x.String())
		c.g.Link(p.URL(), &x, 1/float64(len(links))) // how many links there are in the page is probably proportional to the likely hood of this thing being visited
	}
	return nil
}

func processpage(ctx context.Context, u url.URL, r io.Reader, c *Crawler) ([]*url.URL, error) {
	links := []*url.URL{}
	z := html.NewTokenizer(r)
	for {
		tt := z.Next()
		err := z.Err()
		if err == io.EOF {
			return links, nil
		} else if err != nil {
			return nil, err
		}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			var follow *url.URL
			// not filtering only for links as indexers also follow and crawl images
			for _, attr := range z.Token().Attr {
				// be nice and don't follow anything that you don't have to
				if attr.Key == "rel" && attr.Val == "nofollow" {
					follow = nil
					break
				}
				matches := false
				for _, accepted := range []string{"href"} { // could add other things here like src or srcset
					if attr.Key == accepted {
						matches = true
						break
					}
				}
				if matches {
					un, err := url.Parse(attr.Val)
					if err != nil {
						continue
					}
					follow = u.ResolveReference(un)
				}
			}
			if follow != nil {
				links = append(links, follow)
			}
		}
	}
}

func (c *Crawler) work() {
	for {
		w := c.q.Pop().(*pagerank.Page)
		if err := c.Index(w); err != nil {
			log.Println(err)
		}
	}
}