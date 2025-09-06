package main

import (
	"net/http"

	"github.com/dvjn/goshut"
)

func main() {
	gs := goshut.New()

	http := &http.Server{
		Addr: ":8080",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Hello, World!"))
		}),
	}

	go http.ListenAndServe()
	gs.Register(http.Shutdown)

	gs.WaitAndShutdown()
}
