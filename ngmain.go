package main

import (
	_ "fmt"
	"github.com/NaihongGuo/flag"
	"github.com/guonaihong/ng/nghttp"
)

func main() {
	conf := flag.String("k", "", "Open the configuration file")
	flag.Parse()

	if len(*conf) == 0 {
		flag.Usage()
		return
	}

	nghttp.Loop(*conf)
}
