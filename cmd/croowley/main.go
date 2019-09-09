package main

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"

	"sevki.org/crawler"
)

func main() {
	crawls := 0
	c := crawler.New(
		nil,
		int(float64(runtime.NumCPU())*1.25),
		// this wil trunc to an int, which is fine.
		// On modern machines that have hyperthreading and what not, a quarter more than the # physical cpus is the most optimal worker number.
		func(u *url.URL) bool {
			if u.Scheme != "https" {
				return true
			}
			crawls += 1
			return !(crawls < 10000 && contains(os.Args[1:], u.Host))
		},
	)
	log.Fatal(http.ListenAndServe(":8080", c))
}
func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}