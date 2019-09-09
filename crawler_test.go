package crawler

import (
	"net/url"
	"testing"
)

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
func TestNewCrawler(t *testing.T) {
	tests := []string{"sevki.io" /* "plan9.io"*/}

	crawls := 0
	skippy := func(u *url.URL) bool {
		if u.Scheme != "https" {
			return true
		}
		crawls += 1
		return !(crawls < 100 && contains(tests, u.Host))
	}
	crawler := New(nil, 1, skippy)

	for _, test := range tests {
		t.Run(test, func(t *testing.T) {
			crawler.crawlSite("https://" + test)
		})
	}
	crawler.q.Wait()
	crawler.g.Walk(func(node string, rank float64) {
		t.Log("Node", node, "has a rank of", rank)
	})
}