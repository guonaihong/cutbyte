package main

import (
	_ "fmt"
	"github.com/guonaihong/cutbyte/http"
	"github.com/guonaihong/flag"
)

func main() {
	conf := flag.String("k", "", "Open the configuration file")
	flag.Parse()

	if len(*conf) == 0 {
		flag.Usage()
		return
	}

	http.Loop(*conf)
}
